package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/praetordev/praetor/pkg/events"
)

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
// or a bare requirements.yml treated as roles) into project-adjacent paths so
// Ansible finds them automatically. galaxyEnvVars selects private servers.
// A missing requirements file is not an error; a failed install is.
func installGalaxyRequirements(projectDir string, galaxyEnvVars []string) error {
	// Collections.
	if f := firstExisting(projectDir, "collections/requirements.yml"); f != "" {
		if err := runGalaxy(galaxyEnvVars, "collection", "install", "-r", f, "-p", filepath.Join(projectDir, "collections")); err != nil {
			return fmt.Errorf("installing collection requirements: %w", err)
		}
	}
	// Roles (roles/requirements.yml preferred; bare requirements.yml is the
	// legacy roles location).
	if f := firstExisting(projectDir, "roles/requirements.yml", "requirements.yml"); f != "" {
		if err := runGalaxy(galaxyEnvVars, "role", "install", "-r", f, "-p", filepath.Join(projectDir, "roles")); err != nil {
			return fmt.Errorf("installing role requirements: %w", err)
		}
	}
	return nil
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
