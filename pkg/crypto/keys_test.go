package crypto

import "testing"

const (
	testKeyA = "0123456789abcdef0123456789abcdef" // 32 bytes
	testKeyB = "ABCDEFGHIJKLMNOPQRSTUVWXYZ012345" // 32 bytes
)

func TestPrimaryKeyRequiredByDefault(t *testing.T) {
	t.Setenv("PRAETOR_SECRET_KEY", "")
	t.Setenv("PRAETOR_ALLOW_INSECURE_DEFAULTS", "")
	if _, err := PrimaryKey(); err == nil {
		t.Fatal("expected error when PRAETOR_SECRET_KEY is unset and insecure defaults are not allowed")
	}
}

func TestPrimaryKeyInsecureDefaultOptIn(t *testing.T) {
	t.Setenv("PRAETOR_SECRET_KEY", "")
	t.Setenv("PRAETOR_ALLOW_INSECURE_DEFAULTS", "true")
	k, err := PrimaryKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k != insecureDefaultKey {
		t.Fatalf("expected insecure default, got %q", k)
	}
}

func TestPrimaryKeyRejectsWrongLength(t *testing.T) {
	t.Setenv("PRAETOR_SECRET_KEY", "too-short")
	if _, err := PrimaryKey(); err == nil {
		t.Fatal("expected error for a non-32-byte key")
	}
}

func TestEncryptDecryptSecretRoundTrip(t *testing.T) {
	t.Setenv("PRAETOR_SECRET_KEY", testKeyA)
	t.Setenv("PRAETOR_SECRET_KEY_OLD", "")
	enc, err := EncryptSecret("hunter2")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := DecryptSecret(enc)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("round-trip mismatch: got %q", got)
	}
}

// TestDecryptSecretRotation proves data encrypted under the previous key is still
// readable once PRAETOR_SECRET_KEY is rotated to a new value and the old one is
// moved to PRAETOR_SECRET_KEY_OLD.
func TestDecryptSecretRotation(t *testing.T) {
	// Encrypt under key A.
	t.Setenv("PRAETOR_SECRET_KEY", testKeyA)
	t.Setenv("PRAETOR_SECRET_KEY_OLD", "")
	enc, err := EncryptSecret("rotate-me")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Rotate: new primary is B, old is A.
	t.Setenv("PRAETOR_SECRET_KEY", testKeyB)
	t.Setenv("PRAETOR_SECRET_KEY_OLD", testKeyA)
	got, err := DecryptSecret(enc)
	if err != nil {
		t.Fatalf("decrypt after rotation: %v", err)
	}
	if got != "rotate-me" {
		t.Fatalf("rotation mismatch: got %q", got)
	}

	// Without the old key configured, the same ciphertext must no longer decrypt.
	t.Setenv("PRAETOR_SECRET_KEY_OLD", "")
	if _, err := DecryptSecret(enc); err == nil {
		t.Fatal("expected failure decrypting old-key data without PRAETOR_SECRET_KEY_OLD")
	}
}

func TestValidateSecrets(t *testing.T) {
	t.Setenv("PRAETOR_SECRET_KEY", testKeyA)
	t.Setenv("PRAETOR_SECRET_KEY_OLD", "")
	t.Setenv("JWT_SECRET", "a-signing-secret")
	if err := ValidateSecrets(true); err != nil {
		t.Fatalf("expected valid config: %v", err)
	}

	// A wrong-length OLD key is a misconfiguration even when the primary is fine.
	t.Setenv("PRAETOR_SECRET_KEY_OLD", "short")
	if err := ValidateSecrets(false); err == nil {
		t.Fatal("expected error for wrong-length PRAETOR_SECRET_KEY_OLD")
	}
}
