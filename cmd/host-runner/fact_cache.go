package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

// factCacheDir is where the jsonfile cache plugin reads/writes per-host facts.
func factCacheDir(jobDir string) string { return filepath.Join(jobDir, "fact_cache") }

// factCacheEnv enables Ansible's jsonfile fact cache, scoped to the job dir, with
// no expiry so preloaded facts are always available.
func factCacheEnv(jobDir string) []string {
	return []string{
		"ANSIBLE_CACHE_PLUGIN=jsonfile",
		"ANSIBLE_CACHE_PLUGIN_CONNECTION=" + factCacheDir(jobDir),
		"ANSIBLE_CACHE_PLUGIN_TIMEOUT=0",
	}
}

// writeCachedFacts seeds the cache directory with facts from previous runs, one
// jsonfile per host (the format the jsonfile plugin expects).
func writeCachedFacts(jobDir string, facts map[string]json.RawMessage) {
	dir := factCacheDir(jobDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("facts: cannot create cache dir: %v", err)
		return
	}
	for host, f := range facts {
		if err := os.WriteFile(filepath.Join(dir, host), f, 0644); err != nil {
			log.Printf("facts: cannot seed %s: %v", host, err)
		}
	}
}

// collectFacts reads the cache directory back into a host -> facts map after the
// play, capturing whatever Ansible gathered/updated.
func collectFacts(jobDir string) map[string]json.RawMessage {
	entries, err := os.ReadDir(factCacheDir(jobDir))
	if err != nil {
		return nil
	}
	out := map[string]json.RawMessage{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(factCacheDir(jobDir), e.Name()))
		if err != nil {
			continue
		}
		out[e.Name()] = json.RawMessage(data)
	}
	return out
}

// postFacts ships gathered facts to the ingestion service, which maps each host
// name to a host_id within the run's inventory and upserts host_facts.
func postFacts(apiURL, runID string, facts map[string]json.RawMessage) {
	if apiURL == "" || len(facts) == 0 {
		return
	}
	body, _ := json.Marshal(map[string]interface{}{"facts": facts})
	url := fmt.Sprintf("%s/api/v1/runs/%s/facts", apiURL, runID)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("facts: post failed: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("facts: posted %d host fact set(s) -> HTTP %d", len(facts), resp.StatusCode)
}
