package fs

import (
	"os"
	"path/filepath"
	"strings"
)

func (s *Backend) resolvePath(clientPath string) (string, bool) {
	if s.fsRoot == "" {
		return "", false
	}

	// Normalize client path and join with fs root
	clientPath = strings.TrimLeft(clientPath, "/\\")
	// Convert backslashes to forward slashes before FromSlash
	clientPath = filepath.Clean(filepath.FromSlash(strings.ReplaceAll(clientPath, "\\", "/")))
	joined := filepath.Join(s.fsRoot, clientPath)
	resolved, err := filepath.Abs(joined)
	if err != nil {
		return "", false
	}

	// Ensure resolved path is still under fs root
	cleanFsRoot := filepath.Clean(s.fsRoot)
	// Trim trailing separator for consistent comparison (Clean adds it to roots)
	comparableRoot := strings.TrimSuffix(cleanFsRoot, string(os.PathSeparator))
	if !strings.HasPrefix(resolved, comparableRoot+string(os.PathSeparator)) && resolved != cleanFsRoot {
		return "", false
	}
	return resolved, true
}

func (s *Backend) pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
