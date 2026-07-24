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
	PlatformVersion string `yaml:"platformVersion"`
	ReleaseStatus   string `yaml:"releaseStatus"`
	Image           struct {
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

type quotaSafeDeploymentValues struct {
	DeploymentStrategy struct {
		Type          string `yaml:"type"`
		RollingUpdate struct {
			MaxSurge       int `yaml:"maxSurge"`
			MaxUnavailable int `yaml:"maxUnavailable"`
		} `yaml:"rollingUpdate"`
	} `yaml:"deploymentStrategy"`
	Migrator struct {
		TTLSecondsAfterFinished int `yaml:"ttlSecondsAfterFinished"`
		Resources               struct {
			Requests map[string]string `yaml:"requests"`
			Limits   map[string]string `yaml:"limits"`
		} `yaml:"resources"`
	} `yaml:"migrator"`
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

func TestLocalDevelopmentUpdatePreservesExternalImageOwnership(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "update-local-cluster.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		`go run ./cmd/compatcheck -output helm-values`,
		`go run ./cmd/compatcheck -output images`,
		`docker pull "$image"`,
		`docker build --provenance=false -t "$local_image" -`,
		`k3d image import -c "$CLUSTER" "$image"`,
		`k3d reported success but '$expected' is absent from the node`,
		`authenticate Docker to ghcr.io`,
		`imageRegistries:`,
		`api: ""`,
		`migrator: ""`,
		`scheduler: ""`,
		`ui: ""`,
		`-f "$PLATFORM_VALUES"`,
		`-f "$LOCAL_IMAGE_VALUES"`,
		`image: praetor-$image:$TAG`,
		`--rawfile desired "$DESIRED_IMAGES"`,
		`index($status.image)`,
		`ErrImagePull`,
		`ImagePullBackOff`,
		`InvalidImageName`,
		`CreateContainerConfigError`,
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("local update must contain %q", required)
		}
	}
	for _, forbidden := range []string{
		`--set-string image.tag=`,
		`--set image.tag=`,
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("local update must not contain global override %q", forbidden)
		}
	}

	helperRaw, err := os.ReadFile(filepath.Join(root, "deployments", "helm", "praetor-v2", "templates", "_helpers.tpl"))
	if err != nil {
		t.Fatal(err)
	}
	helper := string(helperRaw)
	if !strings.Contains(helper, `hasKey ($root.Values.imageRegistries | default dict) .svc`) {
		t.Fatal("chart image helper must distinguish an explicit empty per-service registry from an absent override")
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
		`notification-policies?resource_type=`,
		`ensure_policy workflow_template`,
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
	triggerEnd := strings.Index(workflow, "\npermissions:")
	if triggerEnd < 0 {
		t.Fatal("product validation workflow is missing its permissions boundary")
	}
	if strings.Contains(workflow[:triggerEnd], "\n  push:") {
		t.Fatal("product validation must not rerun after a PR has been merged to main")
	}
	for _, required := range []string{"concurrency:", `cancel-in-progress: ${{ github.event_name == 'pull_request' }}`, "packages: read", "docker/login-action@af1e73f918a031802d376d3c8bbc3fe56130a9b0", "fetch-depth: 0", "Plan targeted product journeys", `if: needs.preflight.outputs.run_cluster == 'true'`, `EVENT_NAME: ${{ github.event_name }}`, "plan-product-validation.sh", "needs: preflight", "Run local-equivalent fast product gates", "check-product-validation-fast.sh", `PRAETOR_VALIDATION_USE_RELEASED_COMPONENTS: "true"`, "delegated-api-only:", "Product journey to validate"} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("clean fixture workflow acceleration contract must contain %q", required)
		}
	}
	for _, required := range []string{"k3d cluster create praetor-validation", "bootstrap-product-validation-base.sh", "validate-ldap-operator-journey.sh", "validate-execution-recovery-e2e.sh", "validate-notification-delivery-e2e.sh", "test-secrets-execution-e2e.sh", "validate-delegated-api-e2e.sh", "validate-fleet-scale-e2e.sh", "generate-readiness-report.sh", "actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a", "product-validation-fixture.sh cleanup", "product-validation-fixture.sh status", "statefulset/praetor-executor", "deployment/praetor-scheduler"} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("clean fixture workflow must contain %q", required)
		}
	}
	if strings.Count(workflow, "github.event.pull_request.head.sha || github.sha") != 2 {
		t.Fatal("validation SCM journeys must use the immutable PR head SHA with a push fallback")
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
	for _, required := range []string{"RESUMED_FROM_CHECKPOINT", "recovery-side-effects.log", "deployment/praetor-ingestion --replicas=0", "sh -c 'kill -STOP -1; kill -STOP 1'", "state='reconciling'", "activity-stream?limit=500", "resolution_count", "notification_count", "notification-policies", "resource_type:\"workflow_template\"", "resource_type:\"job_template\"", "notification-kind-isolation", "job-template-notification-exact-once", "team_id:$team", "env PGPASSWORD=validation-only psql -U postgres -d postgres -Atc \"$RESOLUTION_QUERY\"", "PRAETOR_RECOVERY_EVIDENCE_FILE"} {
		if !strings.Contains(recovery, required) {
			t.Fatalf("execution recovery journey must contain %q", required)
		}
	}
	stopAt := strings.Index(recovery, `sh -c 'kill -STOP -1; kill -STOP 1'`)
	removeAt := strings.Index(recovery, `rm -rf "/var/lib/praetor/jobs/$LOST_RUN_ID"`)
	deleteAt := strings.LastIndex(recovery, `kubectl delete pod -n "$NAMESPACE" "$EXECUTOR_POD"`)
	if stopAt < 0 || removeAt < 0 || deleteAt < 0 || !(stopAt < removeAt && removeAt < deleteAt) {
		t.Fatal("unrecoverable recovery fixture must quiesce the executor before removing the WAL and replacing the pod")
	}
	rolloutAt := strings.Index(recovery, `wait_rollout "statefulset/$RELEASE-executor"`)
	stageRunnerAt := strings.Index(recovery, `kubectl cp "$WORK/praetor-host-runner"`)
	stageCallbackAt := strings.Index(recovery, `kubectl cp "$EXECUTOR_ROOT/deploy/plugins/callback/praetor_checkpoint.py"`)
	verifyCallbackAt := strings.Index(recovery, `test -f /opt/praetor/packs/ansible-runtime/plugins/callback/praetor_checkpoint.py`)
	if rolloutAt < 0 || stageRunnerAt < 0 || stageCallbackAt < 0 || verifyCallbackAt < 0 || !(rolloutAt < stageRunnerAt && stageRunnerAt < stageCallbackAt && stageCallbackAt < verifyCallbackAt) {
		t.Fatal("recovery fixture must roll the executor before staging and verifying the candidate checkpoint runtime")
	}
	journeyRaw, err := os.ReadFile(filepath.Join(root, "scripts", "validate-ldap-operator-journey.sh"))
	if err != nil {
		t.Fatal(err)
	}
	journey := string(journeyRaw)
	for _, required := range []string{"demo-operator", "mwebb", "fwalsh", "demo-auditor", "expected 403", "requested_by", "activity-stream", "workflow finished with status", "PRAETOR_LDAP_EVIDENCE_FILE", "wait_notification", "approval-notification-exact-once", "approved-notification-exact-once", "notification-resource-identity"} {
		if !strings.Contains(journey, required) {
			t.Fatalf("LDAP operator journey must contain %q", required)
		}
	}
	bootstrapRaw, err := os.ReadFile(filepath.Join(root, "scripts", "bootstrap-product-validation-base.sh"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := string(bootstrapRaw)
	for _, required := range []string{"PRAETOR_VALIDATION_USE_RELEASED_COMPONENTS", "released_component_ref", "deployments/staging/release-lock.yaml", `docker buildx imagetools inspect --raw "$released_ref"`, `.platform.architecture == $arch`, `docker pull "$platform_ref"`, `docker tag "$platform_ref"`, `released_pids+=("$!")`, "released_pull_failed", "stage-validation-execution-pack.sh"} {
		if !strings.Contains(bootstrap, required) {
			t.Fatalf("clean fixture bootstrap acceleration contract must contain %q", required)
		}
	}
	for _, checkout := range []string{"Check out Scheduler", "Check out Ingestion", "Check out Consumer", "Check out Reconciler"} {
		if strings.Contains(workflow, checkout) {
			t.Fatalf("clean fixture workflow must not rebuild unchanged sibling source through %q", checkout)
		}
	}
	for _, required := range []string{"docker build", `for image in "${validation_images[@]}"`, `k3d image import --mode direct --cluster "$CLUSTER" "$image"`, "praetor-secrets:validation", "praetor-api:$validation_tag", "praetor-migrator:$validation_tag", "praetor-ui:$validation_tag", "praetor-scheduler:$validation_tag", "praetor-executor:$validation_tag", "praetor-ingestion:$validation_tag", "praetor-consumer:$validation_tag", "praetor-reconciler:$validation_tag", "praetor-secrets.image.repository", "praetor-audit-sink.image.repository", "--set image.tag", `--set hostRunner.callbackUrl="http://praetor-ingestion:8081"`} {
		if !strings.Contains(bootstrap, required) {
			t.Fatalf("clean fixture bootstrap must contain %q", required)
		}
	}
	fixtureRaw, err := os.ReadFile(filepath.Join(root, "deployments", "product-validation", "fixture.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fixture := string(fixtureRaw)
	for _, required := range []string{"log_format notification escape=none '$request_body'", "rewrite ^ /capture break", "proxy_pass http://127.0.0.1:8080", "location = /capture { access_log off; return 204; }", "location = /permanent { return 400; }", "praetor-validation-notification-sink"} {
		if !strings.Contains(fixture, required) {
			t.Fatalf("notification recorder must contain %q", required)
		}
	}
}

func TestProductValidationScopeClassifier(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "scripts", "classify-product-validation.sh")
	tests := []struct {
		name, event, paths, want string
	}{
		{"main always runs", "push", "docs/readme.md\n", "true"},
		{"manual always runs", "workflow_dispatch", "", "true"},
		{"isolated chart contract", "pull_request", "deployments/helm/praetor-v2/values.yaml\n", "false"},
		{"isolated UI", "pull_request", "web/pages/JobsPage.tsx\n", "false"},
		{"validation workflow", "pull_request", ".github/workflows/product-validation-fixture.yml\n", "true"},
		{"LDAP journey", "pull_request", "scripts/validate-ldap-operator-journey.sh\n", "true"},
		{"recovery implementation", "pull_request", "internal/readiness/report.go\n", "true"},
		{"notification delivery journey", "pull_request", "scripts/validate-notification-delivery-e2e.sh\n", "true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", script)
			cmd.Env = append(os.Environ(), "EVENT_NAME="+tc.event)
			cmd.Stdin = strings.NewReader(tc.paths)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("classifier failed: %v: %s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != tc.want {
				t.Fatalf("classifier returned %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProductValidationJourneyPlanner(t *testing.T) {
	root := repositoryRoot(t)
	script := filepath.Join(root, "scripts", "plan-product-validation.sh")
	tests := []struct {
		name, event, journey, paths string
		want                        map[string]string
	}{
		{"dynamic PR is focused", "pull_request", "all", "scripts/validate-dynamic-inventory-e2e.sh\n", map[string]string{"run_cluster": "true", "run_dynamic": "true", "run_ldap": "false", "run_readiness": "false"}},
		{"notification PR is focused", "pull_request", "all", "scripts/validate-notification-delivery-e2e.sh\n", map[string]string{"run_cluster": "true", "run_notification": "true", "run_dynamic": "false", "run_readiness": "false"}},
		{"generic fixture PR is complete", "pull_request", "all", "deployments/product-validation/fixture.yaml\n", map[string]string{"run_cluster": "true", "run_dynamic": "true", "run_ldap": "true", "run_readiness": "true"}},
		{"delegated manual avoids cluster", "workflow_dispatch", "delegated-api", "", map[string]string{"run_cluster": "false", "run_delegated": "true", "run_readiness": "false"}},
		{"fleet manual includes deployed fixture", "workflow_dispatch", "fleet-scale", "", map[string]string{"run_cluster": "true", "run_fixture": "true", "run_fleet": "true", "run_readiness": "false"}},
		{"release manual is complete", "workflow_dispatch", "all", "", map[string]string{"run_cluster": "true", "run_dynamic": "true", "run_delegated": "true", "run_readiness": "true"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", script)
			cmd.Env = append(os.Environ(), "EVENT_NAME="+tc.event, "JOURNEY="+tc.journey)
			cmd.Stdin = strings.NewReader(tc.paths)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("planner failed: %v: %s", err, out)
			}
			got := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if ok {
					got[key] = value
				}
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %q, want %q; full plan: %s", key, got[key], want, out)
				}
			}
		})
	}
}

func TestCIExecutesIsolatedGatesInParallel(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(raw)
	for _, required := range []string{"  classify:\n", "  go:\n", "  deployment-contracts:\n", "  ui:\n", "  database-compatibility:\n", "needs: [classify, go, deployment-contracts, ui, database-compatibility]", "Require every selected gate", `${{ needs.classify.result }}`, `${{ needs.go.result }}`, `${{ needs.deployment-contracts.result }}`, `${{ needs.ui.result }}`, `${{ needs.database-compatibility.result }}`} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("CI isolation contract must contain %q", required)
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
		`cpu: 10m`,
		`memory: 16Mi`,
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
		"--force-conflicts --rollback-on-failure --wait",
		"praetor-staging-runtime",
		"praetor-staging-registry",
		"praetor-staging-ingress-tls",
		"praetor-staging-ldap-tls",
		"praetor-staging-ldap-config",
		"praetor-api-identity",
		"deployment/praetor-secrets",
		"missing or has an empty key",
		"expected migrator image is absent from Helm release",
		"helm get manifest",
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
	var lock struct {
		PlatformVersion string `yaml:"platformVersion"`
		Components      map[string]struct {
			Digest string `yaml:"digest"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(lockRaw, &lock); err != nil {
		t.Fatalf("parse staging release lock: %v", err)
	}
	manifestRaw, err := os.ReadFile(filepath.Join(root, "platform-compatibility.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest localDeployManifest
	if err := yaml.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatalf("parse compatibility manifest: %v", err)
	}
	// A development manifest is allowed to lead the immutable staging lock while
	// its artifacts are being built. cmd/stagingrelease remains fail-closed and
	// will not plan or deploy until verified digests update the lock to the exact
	// declared version. Stable manifests must always match immediately.
	if lock.PlatformVersion != manifest.PlatformVersion && manifest.ReleaseStatus != "development" {
		t.Fatalf("staging platform version = %q, want compatibility version %q", lock.PlatformVersion, manifest.PlatformVersion)
	}
	if len(lock.Components) != len(manifest.Components) {
		t.Fatalf("staging lock has %d components, want %d", len(lock.Components), len(manifest.Components))
	}
	for name, component := range lock.Components {
		if len(component.Digest) != len("sha256:")+64 || !strings.HasPrefix(component.Digest, "sha256:") {
			t.Fatalf("staging component %s has invalid digest %q", name, component.Digest)
		}
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
	for _, required := range []string{"ldaps://", "ca_file:", "bind_password_env: PRAETOR_LDAP_BIND_PASSWORD"} {
		if !strings.Contains(ldap, required) {
			t.Fatalf("staging LDAP configuration must contain %q", required)
		}
	}
	for _, forbidden := range []string{"bind_password:", "ldap://praetor", "insecure_skip_verify"} {
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

func TestQuotaConstrainedDeploymentsAvoidSurgeAndKeepMigratorObservable(t *testing.T) {
	root := repositoryRoot(t)
	for _, relativePath := range []string{
		filepath.Join("deployments", "helm", "praetor-v2", "ci", "values-k3d-local.yaml"),
		filepath.Join("deployments", "staging", "values.yaml"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, relativePath))
		if err != nil {
			t.Fatal(err)
		}
		var values quotaSafeDeploymentValues
		if err := yaml.Unmarshal(raw, &values); err != nil {
			t.Fatalf("decode %s: %v", relativePath, err)
		}
		if values.DeploymentStrategy.Type != "RollingUpdate" ||
			values.DeploymentStrategy.RollingUpdate.MaxSurge != 0 ||
			values.DeploymentStrategy.RollingUpdate.MaxUnavailable != 1 {
			t.Errorf("%s must use a zero-surge rolling update, got %+v", relativePath, values.DeploymentStrategy)
		}
		if values.Migrator.TTLSecondsAfterFinished < 600 {
			t.Errorf("%s migrator TTL = %d, must outlive the deployment pipeline wait", relativePath, values.Migrator.TTLSecondsAfterFinished)
		}
		if values.Migrator.Resources.Requests["cpu"] == "" || values.Migrator.Resources.Limits["cpu"] == "" {
			t.Errorf("%s must bound migrator CPU requests and limits", relativePath)
		}
	}

	for _, name := range []string{"api", "consumer", "ingestion", "reconciler", "scheduler", "ui"} {
		raw, err := os.ReadFile(filepath.Join(root, "deployments", "helm", "praetor-v2", "templates", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), ".Values.deploymentStrategy") {
			t.Errorf("%s deployment must apply the configured rollout strategy", name)
		}
	}

	raw, err := os.ReadFile(filepath.Join(root, "deployments", "helm", "praetor-v2", "templates", "migrator-job.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{".Values.migrator.ttlSecondsAfterFinished", ".Values.migrator.resources"} {
		if !strings.Contains(string(raw), required) {
			t.Errorf("migrator template must use %s", required)
		}
	}
}

func TestStagingRecoveryIsEncryptedIsolatedAndNonDestructive(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-recovery.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, required := range []string{
		"openssl cms -encrypt", "-aes-256-cbc", "SHA256SUMS", "pg_dump -U postgres",
		"praetor_secrets", "praetor_audit", "slapcat -n 1", "nats-jetstream.tar.gz",
		"/var/lib/praetor /opt/praetor/packs /home/praetor/.ssh", "executor-state.tar.gz", "praetor-staging-restore", "pg_restore", "restored Praetor integrity counts",
		"slapadd -u -F /etc/ldap/slapd.d",
		"pg_isready -U postgres -d praetor", "did not become ready within 120 seconds",
		"credential_references", "supported rollback changed protected application-state counts", "locked re-upgrade changed protected application-state counts",
		"duration_seconds", "archive_sha256", "sanitized evidence",
		"helm rollback", "no prior successful Helm revision exists", "staging-release.sh\" deploy",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("staging recovery automation must contain %q", required)
		}
	}
	for _, forbidden := range []string{"kubectl delete namespace", "kubectl delete pvc", "k3d cluster delete", "helm upgrade --force"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("staging recovery automation must not contain destructive operation %q", forbidden)
		}
	}
}

func TestStagingAcceptanceIsScopedRepeatableAndNonDestructive(t *testing.T) {
	root := repositoryRoot(t)
	manifestRaw, err := os.ReadFile(filepath.Join(root, "deployments", "staging", "acceptance.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(manifestRaw)
	for _, required := range []string{
		"name: praetor-staging-acceptance-sink",
		"automountServiceAccountToken: false",
		"@sha256:",
		"readinessProbe:",
		"resources:",
	} {
		if !strings.Contains(manifest, required) {
			t.Fatalf("staging acceptance manifest must contain %q", required)
		}
	}

	scriptRaw, err := os.ReadFile(filepath.Join(root, "scripts", "staging-acceptance.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptRaw)
	for _, required := range []string{
		"plan|seed|status|run",
		"demo-operator mwebb fwalsh demo-auditor",
		"Praetor Validation",
		"expected exactly 1",
		"praetor-staging-delegated-db",
		"delete pod/praetor-staging-delegated-db service/praetor-staging-delegated-db",
		"validate-delegated-api-e2e.sh",
		"validate-fleet-scale-live.sh",
		"notification-delivery",
		"notification-policies?resource_type=workflow_template",
		"team_id:$team",
		"chmod 0600",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("staging acceptance automation must contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"kubectl delete namespace",
		"kubectl delete pvc",
		"k3d cluster delete",
		"helm uninstall",
		"praetor-staging-postgres",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("staging acceptance automation must not contain destructive or persistent-database operation %q", forbidden)
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
