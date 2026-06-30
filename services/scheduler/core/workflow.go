package core

import (
	"context"
	"fmt"
	"log"
)

// processWorkflows advances every running workflow one step per tick: it reaps
// finished node jobs, launches newly-eligible nodes (or skips them), pauses on
// approval gates, and finalizes the workflow when all nodes are terminal.
func (s *Scheduler) processWorkflows() {
	ctx := context.Background()
	var ids []int64
	if err := s.DB.SelectContext(ctx, &ids, `SELECT id FROM workflow_jobs WHERE status='running'`); err != nil {
		return
	}
	for _, id := range ids {
		if err := s.advanceWorkflow(ctx, id); err != nil {
			log.Printf("workflow %d: %v", id, err)
		}
	}
}

type wfNode struct {
	ID            int64  `db:"id"`
	NodeKey       string `db:"node_key"`
	NodeType      string `db:"node_type"`
	Name          string `db:"name"`
	Status        string `db:"status"`
	JobTemplateID *int64 `db:"job_template_id"`
	UnifiedJobID  *int64 `db:"unified_job_id"`
}

type wfEdge struct {
	ParentKey string `db:"parent_key"`
	ChildKey  string `db:"child_key"`
	EdgeType  string `db:"edge_type"`
}

func wfTerminal(st string) bool {
	switch st {
	case "successful", "failed", "skipped", "approved", "rejected":
		return true
	}
	return false
}

// wfEdgeFires reports whether an edge of edgeType is satisfied by a parent that
// finished in parentState. approved counts as success, rejected as failure.
func wfEdgeFires(edgeType, parentState string) bool {
	switch edgeType {
	case "success":
		return parentState == "successful" || parentState == "approved"
	case "failure":
		return parentState == "failed" || parentState == "rejected"
	case "always":
		return parentState == "successful" || parentState == "failed" || parentState == "approved" || parentState == "rejected"
	}
	return false
}

func (s *Scheduler) advanceWorkflow(ctx context.Context, wjID int64) error {
	var wf struct {
		TemplateID int64  `db:"workflow_template_id"`
		Name       string `db:"name"`
	}
	if err := s.DB.GetContext(ctx, &wf,
		`SELECT wj.workflow_template_id, wt.name
		 FROM workflow_jobs wj JOIN workflow_templates wt ON wt.id = wj.workflow_template_id
		 WHERE wj.id=$1`, wjID); err != nil {
		return err
	}
	templateID := wf.TemplateID

	var nodes []wfNode
	if err := s.DB.SelectContext(ctx, &nodes,
		`SELECT wjn.id, wjn.node_key, wjn.node_type, COALESCE(wn.name, '') AS name,
		        wjn.status, wjn.job_template_id, wjn.unified_job_id
		 FROM workflow_job_nodes wjn
		 LEFT JOIN workflow_nodes wn ON wn.workflow_template_id = $1 AND wn.node_key = wjn.node_key
		 WHERE wjn.workflow_job_id = $2`, templateID, wjID); err != nil {
		return err
	}
	byKey := map[string]*wfNode{}
	for i := range nodes {
		byKey[nodes[i].NodeKey] = &nodes[i]
	}

	var edges []wfEdge
	_ = s.DB.SelectContext(ctx, &edges,
		`SELECT parent_key, child_key, edge_type FROM workflow_node_edges WHERE workflow_template_id=$1`, templateID)
	parentsOf := map[string][]wfEdge{}
	for _, e := range edges {
		parentsOf[e.ChildKey] = append(parentsOf[e.ChildKey], e)
	}

	// 1. Reap node jobs that have finished.
	for i := range nodes {
		n := &nodes[i]
		if n.Status == "running" && n.UnifiedJobID != nil {
			var st string
			if err := s.DB.GetContext(ctx, &st, `SELECT status FROM unified_jobs WHERE id=$1`, *n.UnifiedJobID); err == nil {
				if st == "successful" || st == "failed" || st == "error" {
					newSt := "failed"
					if st == "successful" {
						newSt = "successful"
					}
					_, _ = s.DB.ExecContext(ctx, `UPDATE workflow_job_nodes SET status=$1 WHERE id=$2`, newSt, n.ID)
					n.Status = newSt
				}
			}
		}
	}

	// 2. Evaluate pending nodes whose parents have all finished.
	for i := range nodes {
		n := &nodes[i]
		if n.Status != "pending" {
			continue
		}
		parents := parentsOf[n.NodeKey]
		allParentsTerminal := true
		for _, e := range parents {
			if p := byKey[e.ParentKey]; p == nil || !wfTerminal(p.Status) {
				allParentsTerminal = false
				break
			}
		}
		if !allParentsTerminal {
			continue
		}

		fired := len(parents) == 0 // a root node runs unconditionally
		for _, e := range parents {
			if p := byKey[e.ParentKey]; p != nil && wfEdgeFires(e.EdgeType, p.Status) {
				fired = true
				break
			}
		}
		if !fired {
			_, _ = s.DB.ExecContext(ctx, `UPDATE workflow_job_nodes SET status='skipped' WHERE id=$1`, n.ID)
			n.Status = "skipped"
			continue
		}

		if n.NodeType == "approval" {
			_, _ = s.DB.ExecContext(ctx, `UPDATE workflow_job_nodes SET status='awaiting_approval' WHERE id=$1`, n.ID)
			n.Status = "awaiting_approval"
			continue
		}
		if n.JobTemplateID == nil {
			_, _ = s.DB.ExecContext(ctx, `UPDATE workflow_job_nodes SET status='skipped' WHERE id=$1`, n.ID)
			n.Status = "skipped"
			continue
		}

		// Launch the node's job template as an ordinary unified_job.
		var ujtID int64
		if err := s.DB.GetContext(ctx, &ujtID, `SELECT unified_job_template_id FROM job_templates WHERE id=$1`, *n.JobTemplateID); err != nil {
			_, _ = s.DB.ExecContext(ctx, `UPDATE workflow_job_nodes SET status='failed' WHERE id=$1`, n.ID)
			n.Status = "failed"
			continue
		}
		// Name each node job uniquely per run so identical workflows/nodes don't
		// collide in the Jobs list: "<workflow> #<run> / <node>".
		nodeLabel := n.Name
		if nodeLabel == "" {
			nodeLabel = n.NodeKey
		}
		jobName := fmt.Sprintf("%s #%d / %s", wf.Name, wjID, nodeLabel)
		var jobID int64
		if err := s.DB.QueryRowContext(ctx,
			`INSERT INTO unified_jobs (name, unified_job_template_id, status) VALUES ($1,$2,'pending') RETURNING id`,
			jobName, ujtID).Scan(&jobID); err != nil {
			_, _ = s.DB.ExecContext(ctx, `UPDATE workflow_job_nodes SET status='failed' WHERE id=$1`, n.ID)
			n.Status = "failed"
			continue
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE workflow_job_nodes SET status='running', unified_job_id=$1 WHERE id=$2`, jobID, n.ID)
		n.Status = "running"
		jid := jobID
		n.UnifiedJobID = &jid
		log.Printf("workflow %d: launched node %q as job %d", wjID, n.NodeKey, jobID)
	}

	// 3. Finalize the workflow when every node is terminal.
	allTerminal := true
	for i := range nodes {
		if !wfTerminal(nodes[i].Status) {
			allTerminal = false
			break
		}
	}
	if allTerminal {
		anyFail := false
		for i := range nodes {
			if nodes[i].Status == "failed" || nodes[i].Status == "rejected" {
				anyFail = true
			}
		}
		status := "successful"
		if anyFail {
			status = "failed"
		}
		_, _ = s.DB.ExecContext(ctx, `UPDATE workflow_jobs SET status=$1, finished_at=now() WHERE id=$2`, status, wjID)
		log.Printf("workflow %d finished: %s", wjID, status)
	}
	return nil
}
