package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	engine "github.com/praetordev/rbac/v4"
)

func SHA256Verifier(expected string) (engine.Verifier, error) {
	expected = strings.ToLower(strings.TrimSpace(expected))
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return nil, fmt.Errorf("RBAC policy SHA-256 must be 64 hexadecimal characters")
	}
	return func(bundle []byte) ([]byte, error) {
		digest := sha256.Sum256(bundle)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), expected) {
			return nil, fmt.Errorf("RBAC policy SHA-256 mismatch")
		}
		return bundle, nil
	}, nil
}

func NewVerifiedFile(ctx context.Context, resolver Resolver, path, expectedSHA256 string) (*Authorizer, error) {
	verifier, err := SHA256Verifier(expectedSHA256)
	if err != nil {
		return nil, err
	}
	return newWithSource(ctx, resolver, engine.NewFileSource(path), path, "sha256", verifier)
}
