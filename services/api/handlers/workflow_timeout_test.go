package handlers

import (
	"testing"

	"github.com/praetordev/store"
)

func TestValidateWorkflowNodesNormalizesTimeoutPolicy(t *testing.T) {
	nodes := []store.WorkflowNode{
		{NodeType: "approval", ApprovalTimeoutSeconds: 60},
		{NodeType: "job", ApprovalTimeoutSeconds: 60, ApprovalTimeoutAction: "approved"},
	}
	if err := validateWorkflowNodes(nodes); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if nodes[0].ApprovalTimeoutAction != "rejected" {
		t.Fatalf("empty approval timeout action should default to rejected")
	}
	if nodes[1].ApprovalTimeoutSeconds != 0 || nodes[1].ApprovalTimeoutAction != "rejected" {
		t.Fatalf("non-approval timeout policy should be cleared: %+v", nodes[1])
	}
}

func TestValidateWorkflowNodesRejectsInvalidTimeoutPolicy(t *testing.T) {
	for _, node := range []store.WorkflowNode{
		{NodeType: "approval", ApprovalTimeoutSeconds: -1},
		{NodeType: "approval", ApprovalTimeoutSeconds: 60, ApprovalTimeoutAction: "skip"},
	} {
		if err := validateWorkflowNodes([]store.WorkflowNode{node}); err == nil {
			t.Fatalf("expected invalid timeout policy to fail: %+v", node)
		}
	}
}
