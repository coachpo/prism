package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (m BootstrapConfigManager) WriteAtomically(path string, payload []byte) error {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return fmt.Errorf("bootstrap config path is required")
	}
	tempPath, err := createBootstrapConfigTempFile(normalizedPath, payload)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := os.Rename(tempPath, normalizedPath); err != nil {
		return fmt.Errorf("replace bootstrap config file: %w", err)
	}
	cleanup = false
	return nil
}

func (m BootstrapConfigManager) WriteAtomicallyIfAbsent(path string, payload []byte) (bool, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return false, fmt.Errorf("bootstrap config path is required")
	}
	tempPath, err := createBootstrapConfigTempFile(normalizedPath, payload)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = os.Remove(tempPath)
	}()
	if err := os.Link(tempPath, normalizedPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("create bootstrap config file: %w", err)
	}
	return true, nil
}

func createBootstrapConfigTempFile(path string, payload []byte) (string, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return "", fmt.Errorf("bootstrap config path is required")
	}
	trimmedPayload := bytes.TrimSpace(payload)
	if len(trimmedPayload) == 0 {
		return "", fmt.Errorf("bootstrap config payload is empty")
	}
	if err := os.MkdirAll(filepath.Dir(normalizedPath), bootstrapDirectoryPermissions); err != nil {
		return "", fmt.Errorf("create bootstrap config directory: %w", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(normalizedPath), "."+filepath.Base(normalizedPath)+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create bootstrap config temp file: %w", err)
	}
	tempPath := tempFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := tempFile.Write(trimmedPayload); err != nil {
		_ = tempFile.Close()
		return "", fmt.Errorf("write bootstrap config temp file: %w", err)
	}
	if _, err := tempFile.Write([]byte("\n")); err != nil {
		_ = tempFile.Close()
		return "", fmt.Errorf("finalize bootstrap config temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return "", fmt.Errorf("sync bootstrap config temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("close bootstrap config temp file: %w", err)
	}
	cleanup = false
	return tempPath, nil
}
