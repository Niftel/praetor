package credentials

import "testing"

func TestRenderInjector(t *testing.T) {
	vals := map[string]string{
		"access_key": "AKIA123",
		"secret_key": "s3cr3t",
	}
	cases := []struct {
		name, tmpl, want string
	}{
		{"simple", "{{ access_key }}", "AKIA123"},
		{"no spaces", "{{access_key}}", "AKIA123"},
		{"embedded", "prefix-{{ secret_key }}-suffix", "prefix-s3cr3t-suffix"},
		{"multiple", "{{ access_key }}:{{ secret_key }}", "AKIA123:s3cr3t"},
		{"unknown field renders empty", "{{ security_token }}", ""},
		{"literal passthrough", "serviceaccount", "serviceaccount"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renderInjector(c.tmpl, vals); got != c.want {
				t.Fatalf("renderInjector(%q) = %q, want %q", c.tmpl, got, c.want)
			}
		})
	}
}
