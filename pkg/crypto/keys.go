package crypto

import (
	"fmt"
	"os"
)

// insecureDefaultKey is the historical hardcoded development key. Data encrypted
// before secrets management was introduced used this value, so it stays here as
// an explicit, opt-in development fallback rather than a silent default scattered
// across the codebase.
const insecureDefaultKey = "12345678901234567890123456789012"

// insecureDefaultJWT is the historical hardcoded JWT signing secret.
const insecureDefaultJWT = "praetor-secret-key-change-me"

// allowInsecureDefaults reports whether the operator has opted into the insecure
// built-in development secrets (PRAETOR_ALLOW_INSECURE_DEFAULTS=true). In any
// other case a missing secret is a hard error so production never silently boots
// with a known key.
func allowInsecureDefaults() bool {
	return os.Getenv("PRAETOR_ALLOW_INSECURE_DEFAULTS") == "true"
}

// PrimaryKey returns the configured at-rest encryption key. It requires
// PRAETOR_SECRET_KEY to be set to a 32-byte value. The insecure built-in default
// is only returned when PRAETOR_ALLOW_INSECURE_DEFAULTS=true.
func PrimaryKey() (string, error) {
	k := os.Getenv("PRAETOR_SECRET_KEY")
	if k == "" {
		if allowInsecureDefaults() {
			return insecureDefaultKey, nil
		}
		return "", fmt.Errorf("PRAETOR_SECRET_KEY is not set: provide a 32-byte secret, or set PRAETOR_ALLOW_INSECURE_DEFAULTS=true for local development")
	}
	if len(k) != 32 {
		return "", fmt.Errorf("PRAETOR_SECRET_KEY must be exactly 32 bytes, got %d", len(k))
	}
	return k, nil
}

// previousKey returns the optional prior encryption key used during a rotation.
// When set (PRAETOR_SECRET_KEY_OLD), DecryptSecret falls back to it so data
// encrypted under the old key is still readable while it is being re-encrypted.
func previousKey() string {
	return os.Getenv("PRAETOR_SECRET_KEY_OLD")
}

// JWTSecret returns the configured JWT signing secret (JWT_SECRET), requiring it
// to be set unless insecure defaults are explicitly allowed.
func JWTSecret() (string, error) {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		if allowInsecureDefaults() {
			return insecureDefaultJWT, nil
		}
		return "", fmt.Errorf("JWT_SECRET is not set: provide a signing secret, or set PRAETOR_ALLOW_INSECURE_DEFAULTS=true for local development")
	}
	return s, nil
}

// EncryptSecret encrypts plainText with the configured primary key.
func EncryptSecret(plainText string) (string, error) {
	key, err := PrimaryKey()
	if err != nil {
		return "", err
	}
	return Encrypt(plainText, key)
}

// DecryptSecret decrypts cipherText with the primary key, transparently falling
// back to the previous key (PRAETOR_SECRET_KEY_OLD) so a key rotation does not
// require re-encrypting everything up front.
func DecryptSecret(cipherText string) (string, error) {
	key, err := PrimaryKey()
	if err != nil {
		return "", err
	}
	if out, err := Decrypt(cipherText, key); err == nil {
		return out, nil
	}
	if old := previousKey(); old != "" && len(old) == 32 {
		if out, err := Decrypt(cipherText, old); err == nil {
			return out, nil
		}
	}
	return "", fmt.Errorf("could not decrypt with the current or previous key")
}

// ValidateSecrets confirms the encryption key (and, when requireJWT is set, the
// JWT secret) are configured. Services call this at startup so a misconfigured
// deployment fails fast and loudly instead of corrupting or leaking data.
func ValidateSecrets(requireJWT bool) error {
	if _, err := PrimaryKey(); err != nil {
		return err
	}
	if old := os.Getenv("PRAETOR_SECRET_KEY_OLD"); old != "" && len(old) != 32 {
		return fmt.Errorf("PRAETOR_SECRET_KEY_OLD must be exactly 32 bytes when set, got %d", len(old))
	}
	if requireJWT {
		if _, err := JWTSecret(); err != nil {
			return err
		}
	}
	return nil
}
