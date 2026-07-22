package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatedArchiveFileMode(t *testing.T) {
	tests := []struct {
		name    string
		mode    int64
		want    os.FileMode
		wantErr bool
	}{
		{name: "zero", mode: 0, want: 0},
		{name: "regular permissions", mode: 0o644, want: 0o644},
		{name: "maximum permissions", mode: 0o777, want: 0o777},
		{name: "negative", mode: -1, wantErr: true},
		{name: "one past maximum", mode: 0o1000, wantErr: true},
		{name: "setuid", mode: 0o4755, wantErr: true},
		{name: "setgid", mode: 0o2755, wantErr: true},
		{name: "sticky", mode: 0o1755, wantErr: true},
		{name: "largest input", mode: math.MaxInt64, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validatedArchiveFileMode(test.mode)
			if test.wantErr {
				if err == nil {
					t.Fatalf("validatedArchiveFileMode(%#o) succeeded with %#o", test.mode, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("validatedArchiveFileMode(%#o) = %#o, want %#o", test.mode, got, test.want)
			}
		})
	}
}

func TestFetchArchiveRejectsModeBeforeCreatingFile(t *testing.T) {
	for _, mode := range []int64{-1, 0o1000, 0o4755} {
		t.Run(fmt.Sprintf("mode_%d", mode), func(t *testing.T) {
			destination := t.TempDir()
			target := filepath.Join(destination, "site.yml")

			err := extractProjectArchive(bytes.NewReader(archiveData(t, mode, "unsafe")), destination)
			if err == nil || !strings.Contains(err.Error(), "unsupported file mode") {
				t.Fatalf("fetchArchive mode %#o error = %v", mode, err)
			}
			if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
				t.Fatalf("invalid archive mode reached file creation: %v", statErr)
			}
		})
	}
}

func TestFetchArchiveExtractsValidRestrictedMode(t *testing.T) {
	destination := t.TempDir()

	if err := extractProjectArchive(bytes.NewReader(archiveData(t, 0o640, "---\n- hosts: all\n")), destination); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(destination, "site.yml")
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "---\n- hosts: all\n" {
		t.Fatalf("unexpected extracted content: %q", content)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&^0o640 != 0 {
		t.Fatalf("extracted permissions %#o exceed archive mode 0640", info.Mode().Perm())
	}
}

func TestExtractProjectArchiveRejectsSymlinkEscape(t *testing.T) {
	destination := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(destination, "link")); err != nil {
		t.Fatal(err)
	}

	err := extractProjectArchive(bytes.NewReader(archiveDataForPath(t, "project/link/escaped", 0o600, "unsafe")), destination)
	if err == nil {
		t.Fatal("extractProjectArchive followed a symlink outside its root")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(statErr) {
		t.Fatalf("archive created a file outside its root: %v", statErr)
	}
}

func archiveData(t *testing.T, mode int64, content string) []byte {
	return archiveDataForPath(t, "project/site.yml", mode, content)
}

func archiveDataForPath(t *testing.T, name string, mode int64, content string) []byte {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{Name: name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tarWriter.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return archive.Bytes()
}
