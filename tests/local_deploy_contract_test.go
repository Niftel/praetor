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
	for _, required := range []string{"k3d cluster create praetor-validation", "bootstrap-product-validation-base.sh", "validate-ldap-operator-journey.sh", "product-validation-fixture.sh cleanup", "product-validation-fixture.sh status"} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("clean fixture workflow must contain %q", required)
		}
	}
	journeyRaw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-ldap-operator-journey.sh"))
	if err != nil {
		t.Fatal(err)
	}
	journey := string(journeyRaw)
	for _, required := range []string{"demo-operator", "mwebb", "fwalsh", "demo-auditor", "expected 403", "requested_by", "activity-stream", "workflow finished with status"} {
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
