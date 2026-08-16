package version

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	loadOnce sync.Once
	cached   string
	loadErr  error
)

func Load() (string, error) {
	loadOnce.Do(func() {
		cached, loadErr = LoadFromPath(DefaultPath())
	})

	return cached, loadErr
}

func LoadFromPath(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read backend version: %w", err)
	}

	version := strings.TrimSpace(string(raw))
	if version == "" {
		return "", fmt.Errorf("backend version file is empty: %s", path)
	}

	return version, nil
}

func DefaultPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "VERSION"
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "VERSION"))
}
