// Package packspec is the single definition + validator for an Execution Pack
// spec. It is shared by the packbuilder service, the execpack CLI, and the API
// (which validates a spec before storing it), so every entry point enforces the
// same rules.
//
// The spec declares only the pack's ENGINE — a standalone CPython plus one of
// ansible / ansible-core (pinned) plus any pip module deps. Galaxy collections
// are NOT part of a pack; they come from the project's requirements.yml and are
// installed at run time.
//
// Every field is a typed value the builder composes into a requirements file —
// nothing is interpolated into a shell — and each is validated against a strict
// pattern, so a spec can't smuggle pip flags (e.g. --extra-index-url) or shell
// metacharacters into the build.
package packspec

import (
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Spec is an Execution Pack definition.
type Spec struct {
	Name   string `yaml:"name"`
	Python string `yaml:"python"` // standalone CPython version, e.g. "3.11.9"

	// Exactly one of Ansible / AnsibleCore, each a bare version:
	//   ansible_core: "2.19.11"  -> pip install ansible-core==2.19.11 (engine only)
	//   ansible:      "12.3.0"   -> pip install ansible==12.3.0       (bundled collections)
	Ansible     string `yaml:"ansible,omitempty"`
	AnsibleCore string `yaml:"ansible_core,omitempty"`

	Pip        []string `yaml:"pip,omitempty"`         // module deps (docker, jmespath, ...)
	Arches     []string `yaml:"arches,omitempty"`      // amd64, arm64
	HostRunner string   `yaml:"host_runner,omitempty"` // REQUIRED daemon release to bundle, e.g. v0.5.0
}

var (
	// A bare version: 2-3 dotted numeric components (3.11.9, 2.19.11, 3.11).
	reVersion = regexp.MustCompile(`^\d+(\.\d+){1,2}$`)
	// host-runner is a git release tag; allow an optional leading v.
	reHostRunner = regexp.MustCompile(`^v?\d+\.\d+\.\d+$`)
	// A pip package: name, optional [extras], optional ==version. No spaces, no
	// leading '-' (blocks flag injection), no shell metacharacters.
	rePip = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(\[[A-Za-z0-9,._-]+\])?(==[0-9][A-Za-z0-9.*_-]*)?$`)
)

var allowedArches = map[string]bool{"amd64": true, "arm64": true}

// Parse unmarshals a spec's YAML.
func Parse(y string) (*Spec, error) {
	var s Spec
	if err := yaml.Unmarshal([]byte(y), &s); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	return &s, nil
}

// Validate rejects any spec that isn't a clean, fully-typed engine definition.
func (s *Spec) Validate() error {
	if s.Python != "" && !reVersion.MatchString(s.Python) {
		return fmt.Errorf("python: %q is not a version (e.g. 3.11.9)", s.Python)
	}
	// Exactly one of ansible / ansible_core.
	switch {
	case s.Ansible == "" && s.AnsibleCore == "":
		return fmt.Errorf("set one of ansible or ansible_core (a version, e.g. ansible_core: \"2.19.11\")")
	case s.Ansible != "" && s.AnsibleCore != "":
		return fmt.Errorf("set only one of ansible or ansible_core, not both")
	case s.Ansible != "" && !reVersion.MatchString(s.Ansible):
		return fmt.Errorf("ansible: %q is not a version (e.g. 12.3.0)", s.Ansible)
	case s.AnsibleCore != "" && !reVersion.MatchString(s.AnsibleCore):
		return fmt.Errorf("ansible_core: %q is not a version (e.g. 2.19.11)", s.AnsibleCore)
	}
	for _, p := range s.Pip {
		if !rePip.MatchString(p) {
			return fmt.Errorf("pip %q is not a valid package spec (name[extras][==version]; no flags, spaces, or shell characters)", p)
		}
	}
	for _, a := range s.Arches {
		if !allowedArches[a] {
			return fmt.Errorf("arch %q not supported (allowed: amd64, arm64)", a)
		}
	}
	// host_runner is required and validated: it is the SINGLE source of the daemon
	// version a pack bundles (the packbuilder passes it as a build-arg and the
	// download is checksum-verified). No default — an unset version must fail the
	// build loudly rather than silently bundle a stale release.
	if s.HostRunner == "" {
		return fmt.Errorf("host_runner is required (the daemon release to bundle, e.g. v0.5.0)")
	}
	if !reHostRunner.MatchString(s.HostRunner) {
		return fmt.Errorf("host_runner: %q is not a version (e.g. v0.4.0)", s.HostRunner)
	}
	return nil
}

// AnsibleRequirement returns the pinned pip requirement for the chosen engine.
// Call only after Validate.
func (s *Spec) AnsibleRequirement() string {
	if s.AnsibleCore != "" {
		return "ansible-core==" + s.AnsibleCore
	}
	return "ansible==" + s.Ansible
}

// Requirements returns the requirements.txt lines (engine + pip deps), all
// already validated — safe to write to a file and `pip install -r`.
func (s *Spec) Requirements() []string {
	return append([]string{s.AnsibleRequirement()}, s.Pip...)
}
