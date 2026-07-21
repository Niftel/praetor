package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResumeArgs(t *testing.T) {
	t.Run("no checkpoint -> fresh run", func(t *testing.T) {
		if got := resumeArgs(t.TempDir()); got != nil {
			t.Fatalf("expected nil for a job with no checkpoint, got %v", got)
		}
	})

	t.Run("empty resume_at -> fresh run", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "checkpoint.json"), []byte(`{"resume_at":"","vars":{}}`), 0644)
		if got := resumeArgs(dir); got != nil {
			t.Fatalf("expected nil for empty resume_at, got %v", got)
		}
	})

	t.Run("checkpoint -> start-at-task + restored vars", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "checkpoint.json"),
			[]byte(`{"resume_at":"slow task","vars":{"greeting":{"stdout":"hi"}}}`), 0644)

		got := resumeArgs(dir)
		want := []string{"--start-at-task", "slow task", "-e", "@" + filepath.Join(dir, "restored-vars.json")}
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("resume args:\n got %v\nwant %v", got, want)
		}

		// The restored-vars file must hold just the vars object, ready for -e @.
		data, err := os.ReadFile(filepath.Join(dir, "restored-vars.json"))
		if err != nil {
			t.Fatalf("restored vars not written: %v", err)
		}
		if !strings.Contains(string(data), `"greeting"`) || !strings.Contains(string(data), `"hi"`) {
			t.Fatalf("restored vars missing registered value: %s", data)
		}
	})
}

func TestCheckpointEnv(t *testing.T) {
	jobDir := t.TempDir()

	t.Run("plugin deployed -> callback enabled", func(t *testing.T) {
		pluginDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(pluginDir, "praetor_checkpoint.py"), []byte("# test callback\n"), 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PRAETOR_CALLBACK_PLUGINS", pluginDir)
		env := checkpointEnv(jobDir, "")
		joined := strings.Join(env, "\n")
		for _, want := range []string{
			"ANSIBLE_CALLBACK_PLUGINS=" + pluginDir,
			"ANSIBLE_CALLBACKS_ENABLED=praetor_checkpoint",
			"PRAETOR_CHECKPOINT=" + filepath.Join(jobDir, "checkpoint.json"),
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("env missing %q; got %v", want, env)
			}
		}
	})

	t.Run("plugin absent -> no checkpointing", func(t *testing.T) {
		t.Setenv("PRAETOR_CALLBACK_PLUGINS", filepath.Join(jobDir, "does-not-exist"))
		if env := checkpointEnv(jobDir, ""); env != nil {
			t.Fatalf("expected nil env when plugin dir is absent, got %v", env)
		}
	})
}

func TestCheckpointEnvFindsCallbackFromSelectedRuntime(t *testing.T) {
	root := t.TempDir()
	ansiblePlaybook := filepath.Join(root, "bin", "ansible-playbook")
	pluginDir := filepath.Join(root, "plugins", "callback")
	if err := os.MkdirAll(filepath.Dir(ansiblePlaybook), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "praetor_checkpoint.py"), []byte("# packed callback\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PRAETOR_CALLBACK_PLUGINS", "")

	env := strings.Join(checkpointEnv(t.TempDir(), ansiblePlaybook), "\n")
	if !strings.Contains(env, "ANSIBLE_CALLBACK_PLUGINS="+pluginDir) {
		t.Fatalf("selected runtime callback was not enabled: %s", env)
	}
}

func TestCallbackPluginDirFindsPackedSibling(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "bin", "praetor-host-runner")
	pluginDir := filepath.Join(root, "plugins", "callback")
	if err := os.MkdirAll(filepath.Dir(executable), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "praetor_checkpoint.py"), []byte("# packed callback\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if got := callbackPluginDir(executable); got != pluginDir {
		t.Fatalf("packed callback dir = %q, want %q", got, pluginDir)
	}
	if got := callbackPluginDir(filepath.Join(t.TempDir(), "bin", "praetor-host-runner")); got != "" {
		t.Fatalf("missing packed callback returned %q", got)
	}
}
