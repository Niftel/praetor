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
func checkpointEnv(jobDir, ansiblePlaybook string) []string {
	pluginDir := os.Getenv("PRAETOR_CALLBACK_PLUGINS")
	if pluginDir == "" {
		pluginDir = callbackPluginDir(ansiblePlaybook)
		if pluginDir == "" {
			executable, err := os.Executable()
			if err == nil {
				pluginDir = callbackPluginDir(executable)
			}
		}
		if pluginDir == "" {
			pluginDir = defaultPluginDir
		}
	}
	if !fileExists(filepath.Join(pluginDir, "praetor_checkpoint.py")) {
		return nil
	}
	return []string{
		"ANSIBLE_CALLBACK_PLUGINS=" + pluginDir,
		"ANSIBLE_CALLBACKS_ENABLED=praetor_checkpoint",
		"PRAETOR_CHECKPOINT=" + filepath.Join(jobDir, "checkpoint.json"),
		"PRAETOR_DIAGNOSTIC_EVENTS=" + filepath.Join(jobDir, "diagnostic-events.jsonl"),
	}
}

// callbackPluginDir returns the callback directory bundled beside a packed
// executable: <pack>/bin/<binary> -> <pack>/plugins/callback.
// It returns an empty string when the packed callback is absent so legacy
// installations can fall back to defaultPluginDir.
func callbackPluginDir(executable string) string {
	dir := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "plugins", "callback"))
	if fileExists(filepath.Join(dir, "praetor_checkpoint.py")) {
		return dir
	}
	return ""
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	directory, name := filepath.Split(path)
	if directory == "" {
		directory = "."
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return false
	}
	defer root.Close()
	st, err := root.Stat(name)
	return err == nil && st.Mode().IsRegular()
}

// resumeArgs returns the extra ansible-playbook arguments to resume an
// interrupted play — `--start-at-task <name>` plus `-e @restored-vars.json` —
// when a usable checkpoint exists in jobDir, or nil for a fresh run. The
// returned slice always starts with "--start-at-task" followed by the task
// name, so callers can log resume[1].
func resumeArgs(jobDir string) []string {
	root, err := os.OpenRoot(jobDir)
	if err != nil {
		log.Printf("resume: could not open job root %s: %v", jobDir, err)
		return nil
	}
	defer root.Close()
	cpPath := filepath.Join(jobDir, "checkpoint.json")
	data, err := root.ReadFile("checkpoint.json")
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
			if err := root.WriteFile("restored-vars.json", b, 0o600); err == nil {
				args = append(args, "-e", "@"+varsPath)
			} else {
				log.Printf("resume: could not write restored vars: %v", err)
			}
		}
	}
	return args
}
