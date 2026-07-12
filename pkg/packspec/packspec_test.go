package packspec

import "testing"

func TestValidateAcceptsCleanSpecs(t *testing.T) {
	for _, y := range []string{
		"name: x\npython: \"3.11.9\"\nansible_core: \"2.19.11\"\narches: [arm64, amd64]\npip: [docker==7.1.0, jmespath]\nhost_runner: v0.5.0\n",
		"name: x\nansible: \"12.3.0\"\narches: [arm64]\nhost_runner: v0.5.0\n",
	} {
		s, err := Parse(y)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("expected valid, got: %v\nspec:\n%s", err, y)
		}
	}
}

func TestValidateRejectsBadSpecs(t *testing.T) {
	cases := map[string]string{
		"neither ansible nor core":   "name: x\npython: \"3.11.9\"\n",
		"both ansible and core":      "ansible: \"12.3.0\"\nansible_core: \"2.19.11\"\n",
		"old package-name value":     "ansible: ansible-core\n", // the thing we're removing
		"ansible_core not a version": "ansible_core: latest\n",
		"pip shell injection":        "ansible_core: \"2.19.11\"\npip: [\"docker; rm -rf /\"]\n",
		"pip flag injection":         "ansible_core: \"2.19.11\"\npip: [\"--extra-index-url=http://evil\"]\n",
		"pip with space/args":        "ansible_core: \"2.19.11\"\npip: [\"docker --no-deps\"]\n",
		"bad arch":                   "ansible_core: \"2.19.11\"\narches: [alpine]\n",
		"missing host_runner":        "ansible_core: \"2.19.11\"\narches: [arm64]\n",
		"host_runner not a version":  "ansible_core: \"2.19.11\"\narches: [arm64]\nhost_runner: latest\n",
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			s, err := Parse(y)
			if err != nil {
				return // a parse failure is also a rejection
			}
			if err := s.Validate(); err == nil {
				t.Fatalf("expected rejection for %q, but it validated", name)
			}
		})
	}
}

func TestAnsibleRequirement(t *testing.T) {
	if got := (&Spec{AnsibleCore: "2.19.11"}).AnsibleRequirement(); got != "ansible-core==2.19.11" {
		t.Fatalf("core: got %q", got)
	}
	if got := (&Spec{Ansible: "12.3.0"}).AnsibleRequirement(); got != "ansible==12.3.0" {
		t.Fatalf("full: got %q", got)
	}
}
