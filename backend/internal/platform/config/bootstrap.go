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
	_, snapshot, settings, _, err := m.loadBootstrapConfigDocument(path)
	return snapshot, settings, err
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

func (m BootstrapConfigManager) loadBootstrapConfigDocument(path string) (bootstrapConfigDocument, BootstrapConfigSnapshot, Settings, []byte, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return bootstrapConfigDocument{}, BootstrapConfigSnapshot{}, Settings{}, nil, fmt.Errorf("bootstrap config path is required")
	}
	raw, err := m.resolvedReadFile()(normalizedPath)
	if err != nil {
		return bootstrapConfigDocument{}, BootstrapConfigSnapshot{}, Settings{}, nil, fmt.Errorf("read bootstrap config %q: %w", normalizedPath, err)
	}
	document, err := parseBootstrapConfigDocument(raw)
	if err != nil {
		return bootstrapConfigDocument{}, BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	snapshot, settings, canonicalPayload, err := buildBootstrapConfigSnapshot(normalizedPath, document)
	if err != nil {
		return bootstrapConfigDocument{}, BootstrapConfigSnapshot{}, Settings{}, nil, err
	}
	return document, snapshot, settings, canonicalPayload, nil
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

func bootstrapServerFromSafeValues(values *BootstrapConfigServerValues) *bootstrapServer {
	if values == nil {
		return nil
	}
	return &bootstrapServer{Host: cloneStringPointer(values.Host), Port: cloneIntPointer(values.Port)}
}

func bootstrapDatabaseFromSafeValues(values *BootstrapConfigDatabaseValues, databaseURL *string) *bootstrapDatabase {
	if values == nil {
		return nil
	}
	return &bootstrapDatabase{
		URL:                 cloneStringPointer(databaseURL),
		Pools:               bootstrapDatabasePoolsFromSafeValues(values.Pools),
		ManagementAdmission: bootstrapManagementAdmissionFromSafeValues(values.ManagementAdmission),
	}
}

func bootstrapDatabasePoolsFromSafeValues(values *BootstrapConfigDatabasePoolsValues) *bootstrapDatabasePools {
	if values == nil {
		return nil
	}
	return &bootstrapDatabasePools{
		TotalMaxConns:    cloneIntPointer(values.TotalMaxConns),
		Management:       bootstrapDatabasePoolFromSafeValues(values.Management),
		RuntimeExecution: bootstrapDatabasePoolFromSafeValues(values.RuntimeExecution),
		RuntimeTelemetry: bootstrapDatabasePoolFromSafeValues(values.RuntimeTelemetry),
		RuntimeFeedback:  bootstrapDatabasePoolFromSafeValues(values.RuntimeFeedback),
		Realtime:         bootstrapDatabasePoolFromSafeValues(values.Realtime),
		CacheRefresh:     bootstrapDatabasePoolFromSafeValues(values.CacheRefresh),
		BackgroundJobs:   bootstrapDatabasePoolFromSafeValues(values.BackgroundJobs),
	}
}

func bootstrapDatabasePoolFromSafeValues(values *BootstrapConfigDatabasePoolValues) *bootstrapDatabasePool {
	if values == nil {
		return nil
	}
	return &bootstrapDatabasePool{MaxConns: cloneIntPointer(values.MaxConns), MinIdleConns: cloneIntPointer(values.MinIdleConns)}
}

func bootstrapManagementAdmissionFromSafeValues(values *BootstrapConfigManagementAdmissionValues) *bootstrapManagementAdmission {
	if values == nil {
		return nil
	}
	return &bootstrapManagementAdmission{M2MaxConcurrent: cloneIntPointer(values.M2MaxConcurrent), M3MaxConcurrent: cloneIntPointer(values.M3MaxConcurrent)}
}

func bootstrapRuntimeFromSafeValues(values *BootstrapConfigRuntimeValues, secretEncryptionKey *string) *bootstrapRuntime {
	if values == nil {
		return nil
	}
	return &bootstrapRuntime{
		SecretEncryptionKey: cloneStringPointer(secretEncryptionKey),
		SideEffects:         bootstrapRuntimeSideEffectsFromSafeValues(values.SideEffects),
		Routing:             bootstrapRuntimeRoutingFromSafeValues(values.Routing),
	}
}

func bootstrapRuntimeSideEffectsFromSafeValues(values *BootstrapConfigRuntimeSideEffectsValues) *bootstrapRuntimeSideEffects {
	if values == nil {
		return nil
	}
	return &bootstrapRuntimeSideEffects{AttemptTimeout: cloneStringPointer(values.AttemptTimeout)}
}

func bootstrapRuntimeRoutingFromSafeValues(*BootstrapConfigRuntimeRoutingValues) *bootstrapRuntimeRouting {
	return nil
}

func defaultSafeBootstrapRuntimeRoutingValues() *BootstrapConfigRuntimeRoutingValues {
	return &BootstrapConfigRuntimeRoutingValues{}
}

func bootstrapHTTPFromSafeValues(values *BootstrapConfigHTTPValues) *bootstrapHTTP {
	if values == nil {
		return nil
	}
	return &bootstrapHTTP{CORSAllowedOrigins: cloneStringSlicePointer(values.CORSAllowedOrigins)}
}

func bootstrapAuthFromSafeValues(values *BootstrapConfigAuthValues, jwtSigningKey *string) *bootstrapAuth {
	if values == nil {
		return nil
	}
	return &bootstrapAuth{
		JWTSigningKey:          cloneStringPointer(jwtSigningKey),
		AccessTokenTTLSeconds:  cloneIntPointer(values.AccessTokenTTLSeconds),
		RefreshTokenTTLSeconds: cloneIntPointer(values.RefreshTokenTTLSeconds),
		ResetCodeTTLSeconds:    cloneIntPointer(values.ResetCodeTTLSeconds),
		AccessCookieName:       cloneStringPointer(values.AccessCookieName),
		RefreshCookieName:      cloneStringPointer(values.RefreshCookieName),
		CookieSecure:           cloneBoolPointer(values.CookieSecure),
	}
}

func bootstrapAlertingFromSafeValues(values *BootstrapConfigAlertingValues) *bootstrapAlerting {
	if values == nil {
		return nil
	}
	return &bootstrapAlerting{WebhookURL: cloneStringPointer(values.WebhookURL)}
}

func bootstrapMailFromSafeValues(values *BootstrapConfigMailValues) *bootstrapMail {
	if values == nil || values.Enabled == nil || !*values.Enabled {
		return canonicalDisabledBootstrapMailDocument()
	}
	return &bootstrapMail{
		Enabled: cloneBoolPointer(values.Enabled),
		From:    cloneStringPointer(values.From),
		ReplyTo: cloneStringPointer(values.ReplyTo),
		SMTP:    bootstrapSMTPFromSafeValues(values.SMTP),
	}
}

func bootstrapSMTPFromSafeValues(values *BootstrapConfigMailSMTPValues) *bootstrapSMTP {
	if values == nil {
		return nil
	}
	return &bootstrapSMTP{
		Host:          cloneStringPointer(values.Host),
		Port:          cloneIntPointer(values.Port),
		Mode:          cloneStringPointer(values.Mode),
		EHLOHostname:  cloneStringPointer(values.EHLOHostname),
		Auth:          cloneStringPointer(values.Auth),
		Username:      cloneStringPointer(values.Username),
		PasswordFile:  cloneStringPointer(values.PasswordFile),
		Timeout:       cloneStringPointer(values.Timeout),
		TLSServerName: cloneStringPointer(values.TLSServerName),
	}
}

func isDisabledSafeBootstrapTelemetry(values *BootstrapConfigTelemetryValues) bool {
	return values == nil || values.Enabled == nil || !*values.Enabled
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

func cloneBootstrapConfigDocument(document bootstrapConfigDocument) bootstrapConfigDocument {
	clone := bootstrapConfigDocument{}
	if document.Meta != nil {
		clone.Meta = &bootstrapMeta{SchemaVersion: cloneIntPointer(document.Meta.SchemaVersion), Revision: cloneIntPointer(document.Meta.Revision), CreatedAt: cloneStringPointer(document.Meta.CreatedAt), UpdatedAt: cloneStringPointer(document.Meta.UpdatedAt)}
	}
	clone.Server = bootstrapServerFromSafeValues(safeBootstrapServerValues(document.Server))
	clone.Database = bootstrapDatabaseFromSafeValues(safeBootstrapDatabaseValues(document.Database), currentBootstrapDatabaseURL(&document))
	clone.Runtime = bootstrapRuntimeFromSafeValues(safeBootstrapRuntimeValues(document.Runtime), currentBootstrapRuntimeSecret(&document))
	clone.HTTP = bootstrapHTTPFromSafeValues(safeBootstrapHTTPValues(document.HTTP))
	clone.Auth = bootstrapAuthFromSafeValues(safeBootstrapAuthValues(document.Auth), currentBootstrapAuthJWTSigningKey(&document))
	clone.Alerting = bootstrapAlertingFromSafeValues(safeBootstrapAlertingValues(document.Alerting))
	clone.Mail = bootstrapMailFromSafeValues(safeBootstrapMailValues(document.Mail))
	clone.Telemetry = cloneBootstrapTelemetry(document.Telemetry)
	if document.StateTransfer != nil {
		clone.StateTransfer = &bootstrapStateTransfer{BundleEncryptionKey: cloneStringPointer(document.StateTransfer.BundleEncryptionKey)}
	}
	return clone
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
