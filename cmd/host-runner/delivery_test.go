package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestIsCompleteRequiresDelivery is the guard for the "no ingestion dependency"
// invariant: a job that reached a terminal state but has NOT delivered its WAL to
// the control plane (e.g. it finished while ingestion was unreachable) must NOT be
// treated as complete — otherwise resumeAll skips it and the result is stranded.
func TestIsCompleteRequiresDelivery(t *testing.T) {
	writeTerminal := func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte(`{"state":"successful"}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// events.jsonl with a cursor at `off`. off == size => delivered.
	writeWAL := func(dir, name, cursorName string, size, off int) {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, cursorName), []byte(fmt.Sprintf("%d", off)), 0644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("terminal but WAL undelivered -> not complete", func(t *testing.T) {
		dir := t.TempDir()
		writeTerminal(dir)
		writeWAL(dir, "events.jsonl", "events.cursor", 500, 200) // 300 bytes unshipped
		if jobDelivered(dir) {
			t.Fatal("jobDelivered = true with cursor behind the WAL")
		}
		if isComplete(dir) {
			t.Fatal("isComplete = true for a terminal-but-undelivered job (would strand the result)")
		}
	})

	t.Run("terminal and fully delivered -> complete", func(t *testing.T) {
		dir := t.TempDir()
		writeTerminal(dir)
		writeWAL(dir, "events.jsonl", "events.cursor", 500, 500) // fully shipped
		// no stdout.log => delivered() treats a missing WAL as nothing-to-ship
		if !jobDelivered(dir) {
			t.Fatal("jobDelivered = false when cursor is at WAL end and no stdout log")
		}
		if !isComplete(dir) {
			t.Fatal("isComplete = false for a terminal, fully-delivered job")
		}
	})

	t.Run("terminal, events delivered but stdout log lagging -> not complete", func(t *testing.T) {
		dir := t.TempDir()
		writeTerminal(dir)
		writeWAL(dir, "events.jsonl", "events.cursor", 100, 100)
		writeWAL(dir, "stdout.log", "stdout.cursor", 1000, 10) // logs barely shipped
		if isComplete(dir) {
			t.Fatal("isComplete = true while the stdout log is still undelivered")
		}
	})

	t.Run("not terminal -> not complete regardless of delivery", func(t *testing.T) {
		dir := t.TempDir()
		writeWAL(dir, "events.jsonl", "events.cursor", 10, 10)
		if isComplete(dir) {
			t.Fatal("isComplete = true for a job with no terminal status")
		}
	})
}
