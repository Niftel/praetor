package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatefulSetPreflightAllowsUnchangedImmutableSpec(t *testing.T) {
	output, err := runStatefulSetPreflight(t, "5Gi")
	if err != nil {
		t.Fatalf("unchanged preflight failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "StatefulSet upgrade preflight passed") {
		t.Fatalf("missing success result: %s", output)
	}
}

func TestStatefulSetPreflightBlocksVolumeTemplateChange(t *testing.T) {
	output, err := runStatefulSetPreflight(t, "512Mi")
	if err == nil {
		t.Fatalf("immutable storage change unexpectedly passed: %s", output)
	}
	for _, required := range []string{
		"upgrade would change immutable fields",
		`"storage": "5Gi"`,
		`"storage": "512Mi"`,
		"Helm was not run; no release resources were mutated",
	} {
		if !strings.Contains(output, required) {
			t.Fatalf("preflight output must contain %q:\n%s", required, output)
		}
	}
}

func runStatefulSetPreflight(t *testing.T, desiredStorage string) (string, error) {
	t.Helper()
	root := repositoryRoot(t)
	temp := t.TempDir()
	live := `{"apiVersion":"apps/v1","kind":"StatefulSet","metadata":{"name":"praetor-executor"},"spec":{"serviceName":"praetor-executor","podManagementPolicy":"Parallel","selector":{"matchLabels":{"app.kubernetes.io/name":"praetor","app.kubernetes.io/instance":"praetor","app.kubernetes.io/component":"executor"}},"volumeClaimTemplates":[{"metadata":{"name":"jobs"},"spec":{"accessModes":["ReadWriteOnce"],"resources":{"requests":{"storage":"5Gi"}}}}]}}`
	desired := strings.ReplaceAll(live, `"5Gi"`, `"`+desiredStorage+`"`)

	writeExecutable(t, filepath.Join(temp, "helm"), "#!/usr/bin/env bash\nprintf '%s\\n' 'apiVersion: apps/v1' 'kind: StatefulSet'\n")
	kubectl := `#!/usr/bin/env bash
set -e
if [[ "$1" == get && "$3" == praetor-executor ]]; then printf '%s\n' "$LIVE_JSON"; exit 0; fi
if [[ "$1" == get ]]; then exit 1; fi
if [[ "$1" == create ]]; then cat >/dev/null; printf '%s\n' "$DESIRED_JSON"; exit 0; fi
exit 2
`
	writeExecutable(t, filepath.Join(temp, "kubectl"), kubectl)

	cmd := exec.Command("bash", filepath.Join(root, "scripts", "helm-statefulset-preflight.sh"), "praetor", "praetor", "chart")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+temp+":"+os.Getenv("PATH"), "LIVE_JSON="+live, "DESIRED_JSON="+desired)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
