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
	if nodes[0].ApprovalTimeoutSeconds != approvalExpirySeconds || nodes[0].ApprovalTimeoutAction != "rejected" {
		t.Fatalf("approval policy should be fixed at 24-hour rejection: %+v", nodes[0])
	}
	if nodes[1].ApprovalTimeoutSeconds != 0 || nodes[1].ApprovalTimeoutAction != "rejected" {
		t.Fatalf("non-approval timeout policy should be cleared: %+v", nodes[1])
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
