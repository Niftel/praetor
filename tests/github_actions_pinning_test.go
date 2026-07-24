package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var workflowAction = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*([^\s#]+)`)
var immutableAction = regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)

func TestGitHubActionsUseImmutablePins(t *testing.T) {
	root := repositoryRoot(t)
	workflows := filepath.Join(root, ".github", "workflows")
	err := filepath.WalkDir(workflows, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range workflowAction.FindAllStringSubmatch(string(raw), -1) {
			ref := match[1]
			if strings.HasPrefix(ref, "./") {
				continue
			}
			if !immutableAction.MatchString(ref) {
				t.Errorf("%s uses mutable action reference %q", filepath.Base(path), ref)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGitHubActionBaselineUsesReviewedReleases(t *testing.T) {
	root := repositoryRoot(t)
	required := map[string]string{
		"actions/checkout":        "9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0",
		"actions/setup-go":        "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",
		"actions/attest":          "f7c74d28b9d84cb8768d0b8ca14a4bac6ef463e6",
		"azure/setup-helm":        "9bc31f4ebc9c6b171d7bfbaa5d006ae7abdb4310",
		"actions/upload-artifact": "043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
	}
	seen := make(map[string]bool, len(required))
	err := filepath.WalkDir(filepath.Join(root, ".github", "workflows"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for action, sha := range required {
			if strings.Contains(string(raw), action+"@"+sha) {
				seen[action] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for action := range required {
		if !seen[action] {
			t.Errorf("reviewed action release is not used: %s", action)
		}
	}
}
