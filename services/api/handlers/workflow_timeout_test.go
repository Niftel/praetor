package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
	if nodes[0].ApprovalTimeoutSeconds != approvalExpirySeconds || nodes[0].ApprovalTimeoutAction != "rejected" {
		t.Fatalf("approval policy should be fixed at 24-hour rejection: %+v", nodes[0])
	}
	if nodes[1].ApprovalTimeoutSeconds != 0 || nodes[1].ApprovalTimeoutAction != "rejected" {
		t.Fatalf("non-approval timeout policy should be cleared: %+v", nodes[1])
	}
}

func TestCreateWorkflowRejectsClientApprovalTimeoutPolicy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflow-templates", strings.NewReader(`{
		"organization_id": 1,
		"name": "release",
		"nodes": [{
			"node_key": "approval",
			"node_type": "approval",
			"approval_timeout_seconds": 60,
			"approval_timeout_action": "approved"
		}]
	}`))
	rec := httptest.NewRecorder()

	(&WorkflowsResource{}).CreateWorkflow(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("client timeout policy should be rejected: code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestValidateWorkflowNodesOverridesClientTimeoutPolicy(t *testing.T) {
	for _, node := range []store.WorkflowNode{
		{NodeType: "approval", ApprovalTimeoutSeconds: -1},
		{NodeType: "approval", ApprovalTimeoutSeconds: 60, ApprovalTimeoutAction: "approved"},
	} {
		nodes := []store.WorkflowNode{node}
		if err := validateWorkflowNodes(nodes); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if nodes[0].ApprovalTimeoutSeconds != approvalExpirySeconds || nodes[0].ApprovalTimeoutAction != "rejected" {
			t.Fatalf("client policy should be replaced: %+v", nodes[0])
		}
	}
}
