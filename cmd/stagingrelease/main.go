// stagingrelease validates the immutable staging lock against the authoritative
// platform compatibility manifest and emits deterministic Helm values.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type manifest struct {
	PlatformVersion string               `yaml:"platformVersion"`
	Image           manifestImage        `yaml:"image"`
	Components      map[string]component `yaml:"components"`
}

type manifestImage struct {
	Registry string `yaml:"registry"`
}

type component struct {
	Image   string `yaml:"image"`
	Version string `yaml:"version"`
}

type releaseLock struct {
	SchemaVersion   int                      `yaml:"schemaVersion"`
	PlatformVersion string                   `yaml:"platformVersion"`
	Registry        string                   `yaml:"registry"`
	Components      map[string]lockComponent `yaml:"components"`
}

type lockComponent struct {
	Image   string `yaml:"image"`
	Version string `yaml:"version"`
	Digest  string `yaml:"digest"`
}

func decode(path string, destination any) {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	// The compatibility manifest contains fields owned by compatcheck; this
	// command consumes only its release/image subset. The staging lock itself is
	// deliberately strict so misspelled digest fields cannot be ignored.
	decoder.KnownFields(strings.HasSuffix(path, "release-lock.yaml"))
	if err := decoder.Decode(destination); err != nil {
		fatalf("decode %s: %v", path, err)
	}
}

func main() {
	manifestPath := flag.String("manifest", "platform-compatibility.yaml", "compatibility manifest path")
	lockPath := flag.String("lock", "deployments/staging/release-lock.yaml", "staging lock path")
	output := flag.String("output", "summary", "summary, helm-values, images, or artifacts")
	flag.Parse()

	var declared manifest
	var locked releaseLock
	decode(*manifestPath, &declared)
	decode(*lockPath, &locked)

	var problems []string
	if locked.SchemaVersion != 1 {
		problems = append(problems, fmt.Sprintf("lock schemaVersion must be 1, got %d", locked.SchemaVersion))
	}
	if locked.PlatformVersion != declared.PlatformVersion {
		problems = append(problems, fmt.Sprintf("lock platformVersion %q does not match manifest %q", locked.PlatformVersion, declared.PlatformVersion))
	}
	if strings.TrimSuffix(locked.Registry, "/") != strings.TrimSuffix(declared.Image.Registry, "/") {
		problems = append(problems, fmt.Sprintf("lock registry %q does not match manifest %q", locked.Registry, declared.Image.Registry))
	}
	for name, expected := range declared.Components {
		actual, ok := locked.Components[name]
		if !ok {
			problems = append(problems, "lock is missing component "+name)
			continue
		}
		if actual.Image != expected.Image || actual.Version != expected.Version {
			problems = append(problems, fmt.Sprintf("lock component %s does not match manifest image/version", name))
		}
		if !digestPattern.MatchString(actual.Digest) {
			problems = append(problems, fmt.Sprintf("lock component %s has invalid digest %q", name, actual.Digest))
		}
	}
	for name := range locked.Components {
		if _, ok := declared.Components[name]; !ok {
			problems = append(problems, "lock contains undeclared component "+name)
		}
	}
	if len(problems) != 0 {
		sort.Strings(problems)
		fatalf("invalid staging release lock:\n - %s", strings.Join(problems, "\n - "))
	}

	names := make([]string, 0, len(locked.Components))
	for name := range locked.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	switch *output {
	case "summary":
		fmt.Printf("Praetor staging %s: %d digest-pinned components\n", locked.PlatformVersion, len(names))
	case "helm-values":
		fmt.Println("image:")
		fmt.Printf("  registry: %q\n", strings.TrimSuffix(locked.Registry, "/"))
		fmt.Println(`  tag: ""`)
		fmt.Println("imageTags:")
		for _, name := range names {
			fmt.Printf("  %s: %q\n", name, locked.Components[name].Version)
		}
		fmt.Println("imageDigests:")
		for _, name := range names {
			fmt.Printf("  %s: %q\n", name, locked.Components[name].Digest)
		}
	case "images":
		for _, name := range names {
			component := locked.Components[name]
			fmt.Printf("%s/%s@%s\n", strings.TrimSuffix(locked.Registry, "/"), component.Image, component.Digest)
		}
	case "artifacts":
		for _, name := range names {
			component := locked.Components[name]
			fmt.Printf("%s/%s:%s@%s\n", strings.TrimSuffix(locked.Registry, "/"), component.Image, component.Version, component.Digest)
		}
	default:
		fatalf("unsupported output %q", *output)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
