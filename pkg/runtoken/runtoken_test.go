package runtoken

import "testing"

func TestMintIsDeterministicPerRun(t *testing.T) {
	a := Mint("secret", "run-1")
	b := Mint("secret", "run-1")
	if a == "" {
		t.Fatal("expected a non-empty token")
	}
	if a != b {
		t.Fatalf("same secret+run must mint the same token: %q != %q", a, b)
	}
}

func TestMintBindsToRunID(t *testing.T) {
	if Mint("secret", "run-1") == Mint("secret", "run-2") {
		t.Fatal("tokens for different runs must differ (token must be bound to the run id)")
	}
}

func TestMintBindsToSecret(t *testing.T) {
	if Mint("secret-a", "run-1") == Mint("secret-b", "run-1") {
		t.Fatal("tokens under different secrets must differ")
	}
}

func TestMintEmptySecretYieldsEmpty(t *testing.T) {
	if got := Mint("", "run-1"); got != "" {
		t.Fatalf("empty secret must mint an empty token, got %q", got)
	}
}
