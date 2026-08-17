package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	BootstrapConfigPathEnv = "PRISM_CONFIG_PATH"

	defaultBootstrapConfigPath    = "config.json"
	bootstrapConfigSchemaVersion  = 1
	bootstrapDirectoryPermissions = 0o755
)

type BootstrapConfigManagerOptions struct {
	ReadFile func(string) ([]byte, error)
	TimeNow  func() time.Time
}

type BootstrapConfigManager struct {
	readFile func(string) ([]byte, error)
	timeNow  func() time.Time
}

func NewBootstrapConfigManager(options BootstrapConfigManagerOptions) BootstrapConfigManager {
	return BootstrapConfigManager{readFile: options.ReadFile, timeNow: options.TimeNow}
}

func (m BootstrapConfigManager) Load(path string) (Settings, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return Settings{}, fmt.Errorf("bootstrap config path is required")
	}
	raw, err := m.resolvedReadFile()(normalizedPath)
	if err != nil {
		return Settings{}, fmt.Errorf("read bootstrap config %q: %w", normalizedPath, err)
	}
	return m.Parse(raw)
}

func (m BootstrapConfigManager) LoadFromEnv() (Settings, error) {
	configPath, err := resolveBootstrapExternalInputsFromEnv()
	if err != nil {
		return Settings{}, err
	}
	return m.Load(configPath)
}

func (m BootstrapConfigManager) LoadOrSeed(path string) (Settings, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return Settings{}, fmt.Errorf("bootstrap config path is required")
	}
	if _, err := os.Stat(normalizedPath); err == nil {
		return m.Load(normalizedPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Settings{}, fmt.Errorf("stat bootstrap config %q: %w", normalizedPath, err)
	}

	payload, settings, err := m.seedPayloadFromDefaults()
	if err != nil {
		return Settings{}, fmt.Errorf("create bootstrap config %q from canonical defaults: %w", normalizedPath, err)
	}
	written, err := m.WriteAtomicallyIfAbsent(normalizedPath, payload)
	if err != nil {
		return Settings{}, fmt.Errorf("create bootstrap config %q from canonical defaults: %w", normalizedPath, err)
	}
	if !written {
		return m.Load(normalizedPath)
	}
	return settings, nil
}

func (m BootstrapConfigManager) LoadOrSeedFromEnv() (Settings, error) {
	configPath, err := resolveBootstrapExternalInputsFromEnv()
	if err != nil {
		return Settings{}, err
	}
	return m.LoadOrSeed(configPath)
}

func (m BootstrapConfigManager) LoadBootstrapConfigDocument(path string) (BootstrapConfigSnapshot, Settings, error) {
	return m.loadBootstrapConfigDocument(path)
}

func (m BootstrapConfigManager) Parse(raw []byte) (Settings, error) {
	document, err := parseBootstrapConfigDocument(raw)
	if err != nil {
		return Settings{}, err
	}
	return document.toSettings()
}

func (m BootstrapConfigManager) resolvedReadFile() func(string) ([]byte, error) {
	if m.readFile != nil {
		return m.readFile
	}
	return os.ReadFile
}

func (m BootstrapConfigManager) resolvedTimeNow() func() time.Time {
	if m.timeNow != nil {
		return m.timeNow
	}
	return time.Now
}

func (m BootstrapConfigManager) loadBootstrapConfigDocument(path string) (BootstrapConfigSnapshot, Settings, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return BootstrapConfigSnapshot{}, Settings{}, fmt.Errorf("bootstrap config path is required")
	}
	raw, err := m.resolvedReadFile()(normalizedPath)
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, fmt.Errorf("read bootstrap config %q: %w", normalizedPath, err)
	}
	document, err := parseBootstrapConfigDocument(raw)
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, err
	}
	snapshot, settings, _, err := buildBootstrapConfigSnapshot(normalizedPath, document)
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, err
	}
	return snapshot, settings, nil
}

func buildBootstrapConfigSnapshot(path string, document bootstrapConfigDocument) (BootstrapConfigSnapshot, Settings, []byte, error) {
	if err := document.validateSchema(); err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	if err := document.validateSemantics(); err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	settings, err := document.toSettings()
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	canonicalPayload, err := canonicalBootstrapConfigPayload(document)
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	schemaVersion, err := requiredIntConst("meta.schemaVersion", document.Meta.SchemaVersion, bootstrapConfigSchemaVersion)
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	revision, err := requiredIntMin("meta.revision", document.Meta.Revision, 1)
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	createdAt, err := requiredTrimmedString("meta.createdAt", document.Meta.CreatedAt, 1, 0)
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	updatedAt, err := requiredTrimmedString("meta.updatedAt", document.Meta.UpdatedAt, 1, 0)
	if err != nil {
		return BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	return BootstrapConfigSnapshot{
		ConfigPath:    strings.TrimSpace(path),
		SchemaVersion: schemaVersion,
		FileRevision:  revision,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		DocumentETag:  bootstrapConfigETag(canonicalPayload),
		Values:        safeBootstrapConfigValues(document),
		Secrets:       bootstrapConfigSecretMetadata(document),
	}, settings, cloneBytes(canonicalPayload), nil
}

func defaultSafeBootstrapRuntimeRoutingValues() *BootstrapConfigRuntimeRoutingValues {
	return &BootstrapConfigRuntimeRoutingValues{}
}

func parseBootstrapConfigDocument(raw []byte) (bootstrapConfigDocument, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return bootstrapConfigDocument{}, fmt.Errorf("bootstrap config is empty")
	}
	if err := detectUnsupportedBootstrapFormat(raw); err != nil {
		return bootstrapConfigDocument{}, err
	}
	document, err := decodeBootstrapConfig(raw)
	if err != nil {
		return bootstrapConfigDocument{}, err
	}
	if err := document.validateSchema(); err != nil {
		return bootstrapConfigDocument{}, err
	}
	if err := document.validateSemantics(); err != nil {
		return bootstrapConfigDocument{}, err
	}
	return document, nil
}

func resolveBootstrapExternalInputsFromEnv() (string, error) {
	configPath := strings.TrimSpace(os.Getenv(BootstrapConfigPathEnv))
	if configPath == "" {
		return defaultBootstrapConfigPath, nil
	}
	return configPath, nil
}

func intPointer(value int) *int {
	return &value
}

func stringPointer(value string) *string {
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}

func float64Pointer(value float64) *float64 {
	return &value
}
