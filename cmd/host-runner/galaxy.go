package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/praetordev/praetor/pkg/events"
)

// collectionsCacheRoot is the content-addressed cache of installed Galaxy
// content on the runner host. Reused across jobs so identical requirements are
// downloaded once and resolve to the same content every run.
const collectionsCacheRoot = "/var/lib/praetor/collections-cache"

// galaxyEnv builds the ANSIBLE_GALAXY_SERVER_* environment that points
// ansible-galaxy at configured private Galaxy / Automation Hub servers. It
// returns nil when no servers are configured (public galaxy.ansible.com is used).
func galaxyEnv(servers []events.GalaxyServer) []string {
	var names []string
	var env []string
	for _, s := range servers {
		if s.Name == "" || s.URL == "" {
			continue
		}
		names = append(names, s.Name)
		key := "ANSIBLE_GALAXY_SERVER_" + strings.ToUpper(s.Name)
		env = append(env, key+"_URL="+s.URL)
		if s.Token != "" {
			env = append(env, key+"_TOKEN="+s.Token)
		}
		if s.AuthURL != "" {
			env = append(env, key+"_AUTH_URL="+s.AuthURL)
		}
	}
	if len(names) == 0 {
		return nil
	}
	// Server order is the resolution order ansible-galaxy tries.
	return append(env, "ANSIBLE_GALAXY_SERVER_LIST="+strings.Join(names, ","))
}

// installGalaxyRequirements installs a project's role and collection
// requirements (AWX layout: collections/requirements.yml, roles/requirements.yml,
// or a bare requirements.yml treated as roles) into a content-addressed cache on
// the runner host, keyed by the requirements' hash (plus the Galaxy server
// config). It returns the env that points the play at the cache
// (ANSIBLE_COLLECTIONS_PATH / ANSIBLE_ROLES_PATH); a later job with identical
// requirements reuses the cache and downloads nothing. A missing requirements
// file is not an error; a failed install is.
func installGalaxyRequirements(projectDir string, galaxyEnvVars []string) ([]string, error) {
	colReq := firstExisting(projectDir, "collections/requirements.yml")
	roleReq := firstExisting(projectDir, "roles/requirements.yml", "requirements.yml")
	if colReq == "" && roleReq == "" {
		return nil, nil
	}

	key := requirementsHash(colReq, roleReq, galaxyEnvVars)
	cacheDir := filepath.Join(collectionsCacheRoot, key)
	colPath := filepath.Join(cacheDir, "collections")
	rolePath := filepath.Join(cacheDir, "roles")

	var pathEnv []string
	if colReq != "" {
		pathEnv = append(pathEnv, "ANSIBLE_COLLECTIONS_PATH="+colPath)
	}
	if roleReq != "" {
		pathEnv = append(pathEnv, "ANSIBLE_ROLES_PATH="+rolePath)
	}

	// Cache hit: a completed install for these exact requirements already exists.
	if fileExists(filepath.Join(cacheDir, ".done")) {
		log.Printf("galaxy: cache hit %s (no download)", key)
		return pathEnv, nil
	}

	// Cache miss: install into a temp dir and atomically promote it, so two
	// concurrent first-time installs for the same requirements can't clobber a
	// half-populated cache — whoever finishes first wins and the other reuses it.
	if err := os.MkdirAll(collectionsCacheRoot, 0755); err != nil {
		return nil, fmt.Errorf("galaxy cache root: %w", err)
	}
	tmpDir, err := os.MkdirTemp(collectionsCacheRoot, "tmp-")
	if err != nil {
		return nil, fmt.Errorf("galaxy cache temp: %w", err)
	}
	defer os.RemoveAll(tmpDir) // no-op once promoted (renamed away)

	if colReq != "" {
		if err := runGalaxy(galaxyEnvVars, "collection", "install", "-r", colReq, "-p", filepath.Join(tmpDir, "collections")); err != nil {
			return nil, fmt.Errorf("installing collection requirements: %w", err)
		}
	}
	if roleReq != "" {
		if err := runGalaxy(galaxyEnvVars, "role", "install", "-r", roleReq, "-p", filepath.Join(tmpDir, "roles")); err != nil {
			return nil, fmt.Errorf("installing role requirements: %w", err)
		}
	}
	writeCollectionLock(tmpDir, filepath.Join(tmpDir, "collections"), colReq != "")
	// Mark complete inside the temp dir, then promote atomically.
	_ = os.WriteFile(filepath.Join(tmpDir, ".done"), []byte(key), 0644)
	if err := os.Rename(tmpDir, cacheDir); err != nil {
		log.Printf("galaxy: cache %s already populated by a concurrent run", key)
	} else {
		log.Printf("galaxy: cached install %s", key)
	}
	return pathEnv, nil
}

// requirementsHash derives the cache key from the requirements file contents and
// the Galaxy server config, so a content or hub change re-resolves.
func requirementsHash(colReq, roleReq string, galaxyEnvVars []string) string {
	h := sha256.New()
	for _, f := range []string{colReq, roleReq} {
		if f == "" {
			continue
		}
		if data, err := os.ReadFile(f); err == nil {
			h.Write(data)
		}
	}
	for _, e := range galaxyEnvVars {
		h.Write([]byte(e))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// writeCollectionLock records the resolved collection versions next to the cache
// as requirements.lock — a reproducibility/audit record of exactly what was
// installed. Best-effort.
func writeCollectionLock(cacheDir, colPath string, hasCollections bool) {
	if !hasCollections {
		return
	}
	cmd := exec.Command("ansible-galaxy", "collection", "list", "-p", colPath, "--format", "yaml")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(cacheDir, "requirements.lock"), out, 0644)
}

func firstExisting(dir string, rel ...string) string {
	for _, r := range rel {
		p := filepath.Join(dir, r)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func runGalaxy(extraEnv []string, args ...string) error {
	log.Printf("ansible-galaxy %s", strings.Join(args, " "))
	cmd := exec.Command("ansible-galaxy", args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	log.Printf("ansible-galaxy output:\n%s", out)
	return err
}
