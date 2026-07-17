package main

import "testing"

func TestDigestPattern(t *testing.T) {
	valid := "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !digestPattern.MatchString(valid) {
		t.Fatalf("expected valid digest")
	}
	for _, invalid := range []string{"latest", "sha256:abc", "sha512:" + valid} {
		if digestPattern.MatchString(invalid) {
			t.Fatalf("accepted invalid digest %q", invalid)
		}
	}
}
