package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type localDeployManifest struct {
	Image struct {
		Registry string `yaml:"registry"`
	} `yaml:"image"`
	Components map[string]struct {
		Version string `yaml:"version"`
	} `yaml:"components"`
}

type generatedHelmValues struct {
	Image struct {
		Registry string `yaml:"registry"`
		Tag      string `yaml:"tag"`
	} `yaml:"image"`
	ImageTags map[string]string `yaml:"imageTags"`
}

func TestLocalDeploymentReleaseValuesMatchCompatibilityManifest(t *testing.T) {
	root := repositoryRoot(t)

	raw, err := os.ReadFile(filepath.Join(root, "platform-compatibility.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest localDeployManifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", "./cmd/compatcheck", "-output", "helm-values")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generate Helm values: %v\n%s", err, output)
	}

	var values generatedHelmValues
	if err := yaml.Unmarshal(output, &values); err != nil {
		t.Fatalf("decode generated Helm values: %v\n%s", err, output)
	}
	if values.Image.Registry != manifest.Image.Registry {
		t.Fatalf("registry = %q, want %q", values.Image.Registry, manifest.Image.Registry)
	}
	if values.Image.Tag != "" {
		t.Fatalf("global image tag must be empty so component tags win, got %q", values.Image.Tag)
	}
	if len(values.ImageTags) != len(manifest.Components) {
		t.Fatalf("generated %d image tags, want %d", len(values.ImageTags), len(manifest.Components))
	}
	for name, component := range manifest.Components {
		if values.ImageTags[name] != component.Version {
			t.Errorf("imageTags.%s = %q, want %q", name, values.ImageTags[name], component.Version)
		}
	}
}

func TestLocalDeploymentScriptsRejectMutableDefaults(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"update-local-cluster.sh", "deploy-local-release.sh"} {
		raw, err := os.ReadFile(filepath.Join(root, "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(`PRAETOR_IMAGE_TAG:-dev`)) {
			t.Errorf("%s defaults to mutable dev tag", name)
		}
		if strings.Contains(string(raw), "kubectl rollout restart") {
			t.Errorf("%s compensates for a mutable tag with rollout restart", name)
		}
	}
}

func TestLocalDeploymentRunsStatefulSetPreflightBeforeHelm(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"update-local-cluster.sh", "bootstrap-product-validation-base.sh"} {
		raw, err := os.ReadFile(filepath.Join(root, "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		script := string(raw)
		preflight := strings.Index(script, "helm-statefulset-preflight.sh")
		upgrade := strings.LastIndex(script, "helm upgrade --install praetor")
		if name == "update-local-cluster.sh" {
			upgrade = strings.Index(script, "helm upgrade --install")
		}
		if preflight < 0 || upgrade < 0 || preflight > upgrade {
			t.Fatalf("%s must run StatefulSet preflight before Helm upgrade", name)
		}
	}
}

func TestLocalClusterRequiresBrowserIngress(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "local-cluster.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		`create|status|start|stop|recover`,
		`--port "80:80@loadbalancer"`,
		`--port "443:443@loadbalancer"`,
		`validate_ingress_topology`,
		`Traefik disabled`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("local-cluster.sh must enforce %q", required)
		}
	}

	raw, err = os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(raw)
	if !strings.Contains(makefile, "local-cluster-create:") ||
		!strings.Contains(makefile, "./scripts/local-cluster.sh create") {
		t.Fatal("Makefile must expose the supported ingress-enabled cluster creation command")
	}
}

func TestProductValidationFixtureIsScopedAndIdempotent(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "product-validation-fixture.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		`create|status|cleanup`,
		`--dry-run=client -o yaml | kubectl apply -f -`,
		`app.kubernetes.io/part-of=praetor-validation-fixture`,
		`--reuse-values`,
		`DELETE FROM workflow_templates WHERE name = 'Praetor Validation Workflow'`,
		`persistent platform data and secrets were preserved`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("product validation fixture must contain %q", required)
		}
	}
	for _, forbidden := range []string{"delete namespace", "delete pvc", "helm uninstall"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("fixture cleanup must not contain %q", forbidden)
		}
	}
}

func TestProductValidationFixtureHasCleanEnvironmentGate(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{
		"scripts/bootstrap-product-validation-base.sh",
		"scripts/validate-ldap-operator-journey.sh",
		".github/workflows/product-validation-fixture.yml",
		"deployments/product-validation/base-datastores.yaml",
	} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("clean fixture gate is missing %s: %v", name, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "product-validation-fixture.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	for _, required := range []string{"k3d cluster create praetor-validation", "bootstrap-product-validation-base.sh", "validate-ldap-operator-journey.sh", "validate-execution-recovery-e2e.sh", "test-secrets-execution-e2e.sh", "validate-delegated-api-e2e.sh", "generate-readiness-report.sh", "actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02", "product-validation-fixture.sh cleanup", "product-validation-fixture.sh status"} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("clean fixture workflow must contain %q", required)
		}
	}
	for _, required := range []string{"PRAETOR_E2E_SECRETS_DB_APP: praetor-validation-secrets-postgres", "PRAETOR_E2E_AUDIT_DB_APP: praetor-validation-audit-postgres"} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("readiness workflow must contain %q", required)
		}
	}
	recoveryRaw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-execution-recovery-e2e.sh"))
	if err != nil {
		t.Fatal(err)
	}
	recovery := string(recoveryRaw)
	for _, required := range []string{"RESUMED_FROM_CHECKPOINT", "recovery-side-effects.log", "deployment/praetor-ingestion --replicas=0", "state='reconciling'", "activity-stream?limit=500", "resolution_count", "notification_count", "env PGPASSWORD=validation-only psql -U postgres -d postgres -Atc \"$RESOLUTION_QUERY\"", "PRAETOR_RECOVERY_EVIDENCE_FILE"} {
		if !strings.Contains(recovery, required) {
			t.Fatalf("execution recovery journey must contain %q", required)
		}
	}
	journeyRaw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-ldap-operator-journey.sh"))
	if err != nil {
		t.Fatal(err)
	}
	journey := string(journeyRaw)
	for _, required := range []string{"demo-operator", "mwebb", "fwalsh", "demo-auditor", "expected 403", "requested_by", "activity-stream", "workflow finished with status", "PRAETOR_LDAP_EVIDENCE_FILE"} {
		if !strings.Contains(journey, required) {
			t.Fatalf("LDAP operator journey must contain %q", required)
		}
	}
	bootstrapRaw, err := os.ReadFile(filepath.Join(root, "scripts", "bootstrap-product-validation-base.sh"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := string(bootstrapRaw)
	for _, required := range []string{"docker build", "k3d image import", "praetor-secrets:validation", "praetor-api:$validation_tag", "praetor-migrator:$validation_tag", "praetor-ui:$validation_tag", "praetor-scheduler:$validation_tag", "praetor-executor:$validation_tag", "praetor-ingestion:$validation_tag", "praetor-consumer:$validation_tag", "praetor-reconciler:$validation_tag", "praetor-secrets.image.repository", "praetor-audit-sink.image.repository", "--set image.tag", `--set hostRunner.callbackUrl="http://praetor-ingestion:8081"`} {
		if !strings.Contains(bootstrap, required) {
			t.Fatalf("clean fixture bootstrap must contain %q", required)
		}
	}
	fixtureRaw, err := os.ReadFile(filepath.Join(root, "deployments", "product-validation", "fixture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := string(fixtureRaw)
	for _, required := range []string{"log_format notification escape=none '$request_body'", "rewrite ^ /capture break", "proxy_pass http://127.0.0.1:8080", "location = /capture { access_log off; return 204; }", "praetor-validation-notification-sink"} {
		if !strings.Contains(fixture, required) {
			t.Fatalf("notification recorder must contain %q", required)
		}
	}
}

func TestPersistentStagingEnvironmentIsIsolatedAndIdempotent(t *testing.T) {
	root := repositoryRoot(t)
	scriptRaw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-environment.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, required := range []string{
		`plan|provision|status`,
		`praetor-staging`,
		`PRAETOR_STAGING_DATA_ROOT`,
		`install -d -m 0700`,
		`if cluster_exists; then`,
		`preserving it`,
		`/var/lib/rancher/k3s/storage@server:0`,
		`/var/lib/rancher/k3s/storage@agent:*`,
		`assert_storage_mounts`,
		`--port "$HTTP_PORT:80@loadbalancer"`,
		`--port "$HTTPS_PORT:443@loadbalancer"`,
		`get --raw=/readyz`,
		`wait --for=create deployment/traefik`,
		`rollout status deployment/traefik`,
		`wait --for=create storageclass/local-path`,
		`get storageclass local-path`,
		`storage_probe`,
		`persistentVolumeClaim:`,
		`exec "$probe" -- test -s /probe/health`,
		`runAsNonRoot: true`,
		`allowPrivilegeEscalation: false`,
		`drop: [ALL]`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("persistent staging script must contain %q", required)
		}
	}
	for _, forbidden := range []string{"k3d cluster delete", "kubectl delete namespace", "rm -rf"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("persistent staging automation must not contain %q", forbidden)
		}
	}

	cmd := exec.Command("bash", "scripts/staging-environment.sh", "plan")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("staging dry-run plan failed: %v\n%s", err, output)
	}
	for _, expected := range []string{"Persistent Praetor staging plan", "praetor-staging", "8080:80@loadbalancer", "8443:443@loadbalancer"} {
		if !bytes.Contains(output, []byte(expected)) {
			t.Errorf("staging plan is missing %q:\n%s", expected, output)
		}
	}
}

func TestPersistentStagingNamespaceHasCapacityAndSecurityPolicy(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "deployments", "staging", "namespace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(raw)
	for _, required := range []string{
		"name: praetor-staging",
		"app.kubernetes.io/environment: staging",
		"pod-security.kubernetes.io/enforce: baseline",
		"kind: ResourceQuota",
		"requests.storage: 100Gi",
		"persistentvolumeclaims: \"16\"",
		"kind: LimitRange",
	} {
		if !strings.Contains(manifest, required) {
			t.Fatalf("persistent staging namespace policy must contain %q", required)
		}
	}

	readmeRaw, err := os.ReadFile(filepath.Join(root, "deployments", "staging", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(readmeRaw)
	for _, required := range []string{"Topology and trust boundaries", "not a production high-availability", "There is intentionally no staging teardown target"} {
		if !strings.Contains(readme, required) {
			t.Fatalf("persistent staging runbook must contain %q", required)
		}
	}
}

func TestStagingReleaseIsDigestPinnedAndSecretReferenced(t *testing.T) {
	root := repositoryRoot(t)
	scriptRaw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, required := range []string{
		"docker buildx imagetools inspect",
		"does not publish linux/$target_arch required by staging",
		"every rendered staging workload image must be digest-pinned",
		"--rollback-on-failure --wait",
		"praetor-staging-runtime",
		"praetor-staging-registry",
		"praetor-staging-ingress-tls",
		"praetor-staging-ldap-tls",
		"praetor-staging-ldap-config",
		"praetor-api-identity",
		"deployment/praetor-secrets",
		"missing or has an empty key",
		"revision-$revision.json",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("staging release script must contain %q", required)
		}
	}
	valuesRaw, err := os.ReadFile(filepath.Join(root, "deployments", "staging", "values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secretKey:", "jwtSecret:", "internalToken:"} {
		if strings.Contains(string(valuesRaw), forbidden) {
			t.Fatalf("staging values must not contain secret material field %q", forbidden)
		}
	}
	lockRaw, err := os.ReadFile(filepath.Join(root, "deployments", "staging", "release-lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lockRaw), "platformVersion: 0.1.1") || !strings.Contains(string(lockRaw), "digest: sha256:") {
		t.Fatal("staging release lock must declare platform version and digests")
	}
}

func TestStagingIntegrationsUseTLSAndPersistentState(t *testing.T) {
	root := repositoryRoot(t)
	manifestRaw, err := os.ReadFile(filepath.Join(root, "deployments", "staging", "integrations.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(manifestRaw)
	for _, required := range []string{
		"kind: StatefulSet",
		"name: praetor-staging-ldap",
		"LDAP_TLS, value: \"true\"",
		"secretKeyRef: {name: praetor-staging-runtime, key: PRAETOR_LDAP_BIND_PASSWORD}",
		"secretName: praetor-staging-ldap-tls",
		"volumeClaimTemplates:",
		"name: praetor-staging-secrets-postgres",
		"name: praetor-staging-audit-postgres",
		"@sha256:",
	} {
		if !strings.Contains(manifest, required) {
			t.Fatalf("staging integrations manifest must contain %q", required)
		}
	}

	ldapRaw, err := os.ReadFile(filepath.Join(root, "deployments", "staging", "ldap.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ldap := string(ldapRaw)
	for _, required := range []string{"ldaps://", "ca_file:", "insecure_skip_verify: false", "bind_password_env: PRAETOR_LDAP_BIND_PASSWORD"} {
		if !strings.Contains(ldap, required) {
			t.Fatalf("staging LDAP configuration must contain %q", required)
		}
	}
	for _, forbidden := range []string{"bind_password:", "ldap://praetor"} {
		if strings.Contains(ldap, forbidden) {
			t.Fatalf("staging LDAP configuration must not contain %q", forbidden)
		}
	}

	scriptRaw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-integrations.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, required := range []string{
		"plan|bootstrap|status|verify",
		"mkcert -cert-file",
		"praetor-staging-ingress-tls",
		"praetor-staging-ldap-tls",
		"praetor-dev-bootstrap",
		"secrets-database-url-file",
		"audit-database-url-file",
		"docker buildx imagetools inspect",
		"has no linux/$target_arch manifest required by staging",
		"--from-file=password=",
		"--from-file=ca.crt=",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("staging integration automation must contain %q", required)
		}
	}
	for _, forbidden := range []string{"kubectl delete pvc", "kubectl delete namespace", "k3d cluster delete", "rm -rf"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("staging integration automation must not contain destructive operation %q", forbidden)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "platform-compatibility.yaml")); err != nil {
		t.Fatalf("locate repository root: %v", err)
	}
	return root
}
