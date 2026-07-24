package readiness

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

var (
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
	digestPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var productJourneys = []string{"ldap-operator", "secrets-service", "delegated-api", "execution-recovery", "dynamic-inventory", "fleet-scale"}
var stagingJourneys = []string{"ldap-operator", "secrets-service", "delegated-api", "execution-recovery", "dynamic-inventory", "fleet-scale", "staging-health", "staging-recovery", "ui-acceptance"}
var pilotJourneys = []string{"managed-host-pilot", "managed-host-pilot-faults", "secrets-service"}
var stagingComponents = []string{"api", "consumer", "executor", "ingestion", "migrator", "reconciler", "scheduler", "ui"}

type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	Profile       string            `json:"profile,omitempty"`
	GeneratedAt   string            `json:"generated_at"`
	Revisions     Revisions         `json:"revisions"`
	Journeys      []JourneyEvidence `json:"journeys"`
	Findings      []Finding         `json:"findings"`
}

type Revisions struct {
	Praetor        string            `json:"praetor"`
	SecretsService string            `json:"secrets_service"`
	Fixture        string            `json:"fixture"`
	Components     map[string]string `json:"components,omitempty"`
	ExecutionPack  string            `json:"execution_pack,omitempty"`
	TargetImage    string            `json:"target_image,omitempty"`
}

type JourneyEvidence struct {
	Name              string `json:"name"`
	Result            string `json:"result"`
	EvidenceSHA256    string `json:"evidence_sha256"`
	JustificationCode string `json:"justification_code,omitempty"`
}

type Finding struct {
	ID                        string `json:"id"`
	Category                  string `json:"category"`
	Classification            string `json:"classification"`
	Status                    string `json:"status"`
	Issue                     string `json:"issue"`
	SecurityExceptionReviewed bool   `json:"security_exception_reviewed,omitempty"`
}

type Report struct {
	SchemaVersion int               `json:"schema_version"`
	Profile       string            `json:"profile,omitempty"`
	GeneratedAt   string            `json:"generated_at"`
	Revisions     Revisions         `json:"revisions"`
	Journeys      []JourneyEvidence `json:"journeys"`
	Findings      []Finding         `json:"findings"`
	Decision      Decision          `json:"decision"`
}

type Decision struct {
	Status    string   `json:"status"`
	Rationale string   `json:"rationale"`
	Reasons   []string `json:"reasons"`
}

func Decode(r io.Reader) (Manifest, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode evidence manifest: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return Manifest{}, fmt.Errorf("decode evidence manifest: trailing JSON data")
	}
	return manifest, nil
}

func Generate(manifest Manifest) (Report, error) {
	if manifest.SchemaVersion != SchemaVersion {
		return Report{}, fmt.Errorf("unsupported schema_version %d", manifest.SchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339, manifest.GeneratedAt); err != nil {
		return Report{}, fmt.Errorf("generated_at must be RFC3339: %w", err)
	}

	reasons := make([]string, 0)
	for name, revision := range map[string]string{
		"praetor": manifest.Revisions.Praetor, "secrets_service": manifest.Revisions.SecretsService, "fixture": manifest.Revisions.Fixture,
	} {
		if !revisionPattern.MatchString(revision) {
			reasons = append(reasons, "missing-or-invalid-revision:"+name)
		}
	}

	requiredJourneys := productJourneys
	switch manifest.Profile {
	case "", "product-validation":
	case "staging-release-candidate":
		requiredJourneys = stagingJourneys
		for _, name := range stagingComponents {
			if _, ok := manifest.Revisions.Components[name]; !ok {
				reasons = append(reasons, "missing-component-revision:"+name)
			}
		}
	case "managed-host-pilot":
		requiredJourneys = pilotJourneys
		if !digestPattern.MatchString(strings.TrimPrefix(manifest.Revisions.ExecutionPack, "sha256:")) {
			reasons = append(reasons, "missing-or-invalid-revision:execution_pack")
		}
		if !digestPattern.MatchString(strings.TrimPrefix(manifest.Revisions.TargetImage, "sha256:")) {
			reasons = append(reasons, "missing-or-invalid-revision:target_image")
		}
		for _, name := range stagingComponents {
			if _, ok := manifest.Revisions.Components[name]; !ok {
				reasons = append(reasons, "missing-component-revision:"+name)
			}
		}
	default:
		return Report{}, fmt.Errorf("unsupported profile %q", manifest.Profile)
	}
	for name, digest := range manifest.Revisions.Components {
		if name == "" || !digestPattern.MatchString(strings.TrimPrefix(digest, "sha256:")) {
			reasons = append(reasons, "missing-or-invalid-component-revision:"+name)
		}
	}

	known := make(map[string]bool, len(stagingJourneys)+len(pilotJourneys))
	for _, name := range stagingJourneys {
		known[name] = true
	}
	for _, name := range pilotJourneys {
		known[name] = true
	}
	seen := make(map[string]bool, len(manifest.Journeys))
	journeys := append([]JourneyEvidence(nil), manifest.Journeys...)
	for _, journey := range journeys {
		if !known[journey.Name] {
			return Report{}, fmt.Errorf("unknown journey %q", journey.Name)
		}
		if seen[journey.Name] {
			return Report{}, fmt.Errorf("duplicate journey %q", journey.Name)
		}
		seen[journey.Name] = true
		switch journey.Result {
		case "pass":
			if !digestPattern.MatchString(journey.EvidenceSHA256) {
				reasons = append(reasons, "missing-evidence:"+journey.Name)
			}
		case "fail":
			reasons = append(reasons, "journey-failed:"+journey.Name)
		case "not-applicable":
			if journey.JustificationCode != "feature-not-enabled" && journey.JustificationCode != "platform-not-applicable" && journey.JustificationCode != "superseded-by-scoped-test" {
				return Report{}, fmt.Errorf("journey %q requires an allowed not-applicable justification_code", journey.Name)
			}
		default:
			return Report{}, fmt.Errorf("journey %q has invalid result %q", journey.Name, journey.Result)
		}
	}
	for _, name := range requiredJourneys {
		if !seen[name] {
			reasons = append(reasons, "missing-journey:"+name)
		}
	}

	findings := make([]Finding, len(manifest.Findings))
	copy(findings, manifest.Findings)
	for i := range findings {
		finding := &findings[i]
		if finding.ID == "" {
			return Report{}, fmt.Errorf("finding id is required")
		}
		if finding.Category != "security" && finding.Category != "reliability" && finding.Category != "other" {
			return Report{}, fmt.Errorf("finding %q has invalid category", finding.ID)
		}
		if finding.Classification != "release-blocking" && finding.Classification != "non-blocking" && finding.Classification != "demand-gated" {
			return Report{}, fmt.Errorf("finding %q has invalid classification", finding.ID)
		}
		if finding.Status != "open" && finding.Status != "resolved" {
			return Report{}, fmt.Errorf("finding %q has invalid status", finding.ID)
		}
		if !scopedIssue(finding.Issue) {
			return Report{}, fmt.Errorf("finding %q must link to a scoped Niftel/praetor issue", finding.ID)
		}
		if finding.Category == "security" && finding.Classification != "release-blocking" && !finding.SecurityExceptionReviewed {
			return Report{}, fmt.Errorf("security finding %q must be release-blocking unless an exception was reviewed", finding.ID)
		}
		if finding.Status == "open" && finding.Classification == "release-blocking" {
			reasons = append(reasons, "open-release-blocker:"+finding.ID)
		}
	}

	sort.Slice(journeys, func(i, j int) bool { return journeys[i].Name < journeys[j].Name })
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	sort.Strings(reasons)
	status := "go"
	rationale := "All required journeys passed or were explicitly justified, and no release blockers remain."
	if len(reasons) > 0 {
		status = "no-go"
		rationale = "Production-candidate promotion is blocked until every listed reason is resolved."
	}
	if manifest.Profile == "managed-host-pilot" {
		rationale = "All required managed-host pilot journeys passed and no pilot blockers remain."
		if len(reasons) > 0 {
			rationale = "Managed-host pilot use is blocked until every listed reason is resolved."
		}
	}
	return Report{SchemaVersion: SchemaVersion, Profile: manifest.Profile, GeneratedAt: manifest.GeneratedAt, Revisions: manifest.Revisions, Journeys: journeys, Findings: findings, Decision: Decision{Status: status, Rationale: rationale, Reasons: reasons}}, nil
}

func scopedIssue(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "Niftel" || parts[1] != "praetor" || parts[2] != "issues" {
		return false
	}
	return regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(parts[3])
}
