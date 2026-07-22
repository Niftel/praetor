// Package executionboundary centralizes the privileged process and filesystem
// boundaries used while constructing execution packs.
package executionboundary

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var packNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var allowedExecutables = map[string]struct{}{
	"buildctl": {},
	"docker":   {},
	"git":      {},
}

// ValidatePackName accepts the identifier subset that is safe in artifact
// names, registry paths, and fixed build arguments.
func ValidatePackName(name string) error {
	if !packNamePattern.MatchString(name) {
		return fmt.Errorf("pack name %q must match %s", name, packNamePattern.String())
	}
	return nil
}

// Command constructs a process invocation for an explicitly permitted build
// tool. It never invokes a shell and copies the argument slice so callers cannot
// mutate an invocation after validation.
func Command(executable string, args ...string) (*exec.Cmd, error) {
	if _, permitted := allowedExecutables[executable]; !permitted {
		return nil, fmt.Errorf("executable %q is not permitted", executable)
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("resolve permitted executable %q: %w", executable, err)
	}
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve absolute executable path: %w", err)
		}
	}
	commandArgs := make([]string, 1, len(args)+1)
	commandArgs[0] = path
	commandArgs = append(commandArgs, args...)
	return &exec.Cmd{Path: path, Args: commandArgs}, nil
}

// PrepareOutputDirectory creates a relative output directory beneath workspace.
// os.Root performs the creation without permitting traversal or symlink escape;
// the resolved path is checked again before it is handed to an external tool.
func PrepareOutputDirectory(workspace, candidate string) (string, error) {
	if candidate == "" || filepath.IsAbs(candidate) {
		return "", fmt.Errorf("output directory must be a non-empty relative path")
	}
	clean := filepath.Clean(candidate)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("output directory %q escapes the workspace", candidate)
	}

	workspacePath, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	workspacePath, err = filepath.Abs(workspacePath)
	if err != nil {
		return "", fmt.Errorf("resolve workspace absolute path: %w", err)
	}
	root, err := os.OpenRoot(workspacePath)
	if err != nil {
		return "", fmt.Errorf("open workspace root: %w", err)
	}
	defer root.Close()
	if err := root.MkdirAll(clean, 0o755); err != nil {
		return "", fmt.Errorf("create rooted output directory: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Join(workspacePath, clean))
	if err != nil {
		return "", fmt.Errorf("resolve output directory: %w", err)
	}
	relative, err := filepath.Rel(workspacePath, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("output directory %q escapes the workspace", candidate)
	}
	return resolved, nil
}

// WriteFile writes a fixed relative file through an os.Root, preventing path or
// symlink traversal outside directory.
func WriteFile(directory, name string, data []byte, perm os.FileMode) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return fmt.Errorf("open rooted directory: %w", err)
	}
	defer root.Close()
	if err := root.WriteFile(name, data, perm); err != nil {
		return fmt.Errorf("write rooted file %q: %w", name, err)
	}
	return nil
}

// ReadFile reads a relative file through an os.Root, preventing traversal and
// symlink escape from a checkout or generated workspace.
func ReadFile(directory, name string) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("open rooted directory: %w", err)
	}
	defer root.Close()
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read rooted file %q: %w", name, err)
	}
	return data, nil
}
