package handlers

import (
	"encoding/json"
	"testing"
)

func TestSplitCredentialInputsRedactsOnlySecretFields(t *testing.T) {
	schema := json.RawMessage(`{"fields":[{"id":"username"},{"id":"password","secret":true},{"id":"ssh_private_key","secret":true}]}`)
	inputs := json.RawMessage(`{"username":"demo","password":"s3cret","ssh_private_key":"key"}`)

	service, stored, err := splitCredentialInputs(schema, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if service["password"] != "s3cret" || service["ssh_private_key"] != "key" {
		t.Fatalf("service inputs changed: %#v", service)
	}
	if stored["username"] != "demo" {
		t.Fatalf("non-secret input lost: %#v", stored)
	}
	if stored["password"] != "$encrypted$" || stored["ssh_private_key"] != "$encrypted$" {
		t.Fatalf("secret was not redacted: %#v", stored)
	}
}

func TestSplitCredentialInputsRejectsNonStringValues(t *testing.T) {
	_, _, err := splitCredentialInputs(json.RawMessage(`{"fields":[{"id":"password","secret":true}]}`), json.RawMessage(`{"password":42}`))
	if err == nil {
		t.Fatal("expected non-string input to be rejected")
	}
}
