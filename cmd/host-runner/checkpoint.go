package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// defaultPluginDir is where the bootstrap installs the praetor_checkpoint
// Ansible callback plugin on the host.
const defaultPluginDir = "/usr/local/share/praetor/plugins/callback"

// checkpointEnv returns the environment that enables the praetor_checkpoint
// callback for an ansible-playbook run (so progress + registered vars are
// recorded to checkpoint.json in the job dir). It returns nil if the plugin is
// not deployed, in which case the play still runs — only without task-level
// resume (the job simply re-runs from the top on recovery).
func checkpointEnv(jobDir string) []string {
	pluginDir := os.Getenv("PRAETOR_CALLBACK_PLUGINS")
	if pluginDir == "" {
		pluginDir = defaultPluginDir
	}
	if st, err := os.Stat(pluginDir); err != nil || !st.IsDir() {
		return nil
	}
	return []string{
		"ANSIBLE_CALLBACK_PLUGINS=" + pluginDir,
		"ANSIBLE_CALLBACKS_ENABLED=praetor_checkpoint",
		"PRAETOR_CHECKPOINT=" + filepath.Join(jobDir, "checkpoint.json"),
	}
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}

// resumeArgs returns the extra ansible-playbook arguments to resume an
// interrupted play — `--start-at-task <name>` plus `-e @restored-vars.json` —
// when a usable checkpoint exists in jobDir, or nil for a fresh run. The
// returned slice always starts with "--start-at-task" followed by the task
// name, so callers can log resume[1].
func resumeArgs(jobDir string) []string {
	cpPath := filepath.Join(jobDir, "checkpoint.json")
	data, err := os.ReadFile(cpPath)
	if err != nil {
		log.Printf("resume: no checkpoint at %s: %v", cpPath, err)
		return nil
	}
	var cp struct {
		ResumeAt string                     `json:"resume_at"`
		Vars     map[string]json.RawMessage `json:"vars"`
	}
	if jerr := json.Unmarshal(data, &cp); jerr != nil || cp.ResumeAt == "" {
		log.Printf("resume: checkpoint at %s unusable (err=%v resume_at=%q)", cpPath, jerr, cp.ResumeAt)
		return nil
	}
	log.Printf("resume: checkpoint resume_at=%q (%d vars)", cp.ResumeAt, len(cp.Vars))

	args := []string{"--start-at-task", cp.ResumeAt}

	if len(cp.Vars) > 0 {
		varsPath := filepath.Join(jobDir, "restored-vars.json")
		if b, err := json.Marshal(cp.Vars); err == nil {
			if err := os.WriteFile(varsPath, b, 0644); err == nil {
				args = append(args, "-e", "@"+varsPath)
			} else {
				log.Printf("resume: could not write restored vars: %v", err)
			}
		}
	}
	return args
}
