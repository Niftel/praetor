// compatcheck validates the platform compatibility manifest against the parts
// of the repository that consume it. It deliberately uses only repository
// dependencies so it can run before the rest of CI.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const manifestPath = "platform-compatibility.yaml"

var (
	versionPattern   = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
	migrationPattern = regexp.MustCompile(`^([0-9]{6})_.+\.up\.sql$`)
)

type manifest struct {
	SchemaVersion   int                  `yaml:"schemaVersion"`
	PlatformVersion string               `yaml:"platformVersion"`
	ReleaseStatus   string               `yaml:"releaseStatus"`
	WireContracts   string               `yaml:"wireContracts"`
	Image           imageConfig          `yaml:"image"`
	Components      map[string]component `yaml:"components"`
	Contracts       map[string]string    `yaml:"contracts"`
	Database        databaseConfig       `yaml:"database"`
}

type imageConfig struct {
	Registry string `yaml:"registry"`
	Tag      string `yaml:"tag"`
}

type component struct {
	Repository string `yaml:"repository"`
	Module     string `yaml:"module"`
	Image      string `yaml:"image"`
	Version    string `yaml:"version"`
}

type databaseConfig struct {
	MinimumMigration int `yaml:"minimumMigration"`
	MaximumMigration int `yaml:"maximumMigration"`
}

type chartMetadata struct {
	AppVersion string `yaml:"appVersion"`
}

type chartValues struct {
	ImageTags map[string]string `yaml:"imageTags"`
}

func main() {
	release := flag.Bool("release", false, "enforce stable-release invariants")
	output := flag.String("output", "summary", "output format: summary, images, or contracts")
	flag.Parse()

	var problems []string

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		fatalf("read %s: %v", manifestPath, err)
	}

	var m manifest
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&m); err != nil {
		fatalf("decode %s: %v", manifestPath, err)
	}

	if m.SchemaVersion != 1 {
		problems = append(problems, fmt.Sprintf("schemaVersion must be 1, got %d", m.SchemaVersion))
	}
	if !versionPattern.MatchString(m.PlatformVersion) {
		problems = append(problems, "platformVersion must be semantic version syntax")
	}
	if m.ReleaseStatus != "development" && m.ReleaseStatus != "stable" {
		problems = append(problems, "releaseStatus must be development or stable")
	}
	if !regexp.MustCompile(`^v[1-9][0-9]*$`).MatchString(m.WireContracts) {
		problems = append(problems, "wireContracts must identify a version such as v1")
	} else if _, err := os.Stat(filepath.Join("tests", "contracts", m.WireContracts)); err != nil {
		problems = append(problems, fmt.Sprintf("wire contract fixture directory %s is missing", m.WireContracts))
	}
	if *release {
		if m.ReleaseStatus != "stable" {
			problems = append(problems, "release preflight requires releaseStatus: stable")
		}
	}
	if m.Image.Registry == "" || !versionPattern.MatchString(m.Image.Tag) || m.Image.Tag == "latest" {
		problems = append(problems, "image registry and a non-latest semantic image tag are required")
	}

	requiredComponents := []string{"api", "migrator", "ui", "scheduler", "reconciler", "executor", "ingestion", "consumer"}
	for _, name := range requiredComponents {
		c, ok := m.Components[name]
		if !ok {
			problems = append(problems, "missing component "+name)
			continue
		}
		if c.Repository == "" || c.Image == "" || !versionPattern.MatchString(c.Version) {
			problems = append(problems, name+" must declare repository, image, and semantic version")
		}
	}
	if len(m.Components) != len(requiredComponents) {
		problems = append(problems, fmt.Sprintf("expected %d components, found %d", len(requiredComponents), len(m.Components)))
	}

	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		fatalf("read go.mod: %v", err)
	}
	for module, version := range m.Contracts {
		if !versionPattern.MatchString(version) {
			problems = append(problems, fmt.Sprintf("contract %s has invalid version %s", module, version))
			continue
		}
		needle := []byte(module + " " + version)
		if !bytes.Contains(goMod, needle) {
			problems = append(problems, fmt.Sprintf("contract %s %s does not match go.mod", module, version))
		}
	}

	entries, err := os.ReadDir("db/migrations")
	if err != nil {
		fatalf("read migrations: %v", err)
	}
	maximum := 0
	for _, entry := range entries {
		match := migrationPattern.FindStringSubmatch(entry.Name())
		if len(match) == 0 {
			continue
		}
		n, _ := strconv.Atoi(match[1])
		if n > maximum {
			maximum = n
		}
	}
	if m.Database.MinimumMigration < 1 || m.Database.MaximumMigration < m.Database.MinimumMigration {
		problems = append(problems, "database migration range is invalid")
	}
	if maximum != m.Database.MaximumMigration {
		problems = append(problems, fmt.Sprintf("maximumMigration is %d but latest numbered migration is %d", m.Database.MaximumMigration, maximum))
	}

	chartRaw, err := os.ReadFile("deployments/helm/praetor-v2/Chart.yaml")
	if err != nil {
		fatalf("read supported Helm chart: %v", err)
	}
	var chart chartMetadata
	if err := yaml.Unmarshal(chartRaw, &chart); err != nil {
		fatalf("decode supported Helm chart: %v", err)
	}
	if chart.AppVersion != m.Image.Tag {
		problems = append(problems, fmt.Sprintf("Helm appVersion %s does not match platform fallback tag %s", chart.AppVersion, m.Image.Tag))
	}
	valuesRaw, err := os.ReadFile("deployments/helm/praetor-v2/values.yaml")
	if err != nil {
		fatalf("read supported Helm values: %v", err)
	}
	var values chartValues
	if err := yaml.Unmarshal(valuesRaw, &values); err != nil {
		fatalf("decode supported Helm values: %v", err)
	}
	for name, component := range m.Components {
		if values.ImageTags[name] != component.Version {
			problems = append(problems, fmt.Sprintf("Helm imageTags.%s %q does not match component version %s", name, values.ImageTags[name], component.Version))
		}
	}

	if len(problems) != 0 {
		sort.Strings(problems)
		fmt.Fprintln(os.Stderr, "compatibility manifest is invalid:")
		fmt.Fprintln(os.Stderr, " - "+strings.Join(problems, "\n - "))
		os.Exit(1)
	}

	switch *output {
	case "summary":
		fmt.Printf("Praetor %s (%s): %d components, %d contracts, wire %s, migrations %d-%d\n",
			m.PlatformVersion, m.ReleaseStatus, len(m.Components), len(m.Contracts),
			m.WireContracts, m.Database.MinimumMigration, m.Database.MaximumMigration)
	case "images":
		names := make([]string, 0, len(m.Components))
		for name := range m.Components {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("%s/%s:%s\n", strings.TrimSuffix(m.Image.Registry, "/"), m.Components[name].Image, m.Components[name].Version)
		}
	case "contracts":
		modules := make([]string, 0, len(m.Contracts))
		for module := range m.Contracts {
			modules = append(modules, module)
		}
		sort.Strings(modules)
		for _, module := range modules {
			fmt.Printf("%s@%s\n", module, m.Contracts[module])
		}
	default:
		fatalf("unknown output format %q", *output)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
