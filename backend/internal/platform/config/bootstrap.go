package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"slices"
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

const (
	BootstrapConfigSecretDatabaseURL                        = "database.url"
	BootstrapConfigSecretRuntimeSecretEncryptionKey         = "secretEncryptionKey"
	BootstrapConfigSecretAuthJWTSigningKey                  = "auth.jwtSigningKey"
	BootstrapConfigSecretStateTransferBundleKey             = "stateTransfer.bundleEncryptionKey"
	BootstrapConfigSecretMailSMTPPassword                   = "mail.smtp.password"
	BootstrapConfigSecretTelemetryAuthorizationHeader       = "telemetry.exporter.auth.authorizationHeader"
	BootstrapConfigConfirmationServerHostChange             = "server-host-change"
	BootstrapConfigConfirmationServerPortChange             = "server-port-change"
	BootstrapConfigConfirmationDatabaseURLChange            = "database-url-change"
	BootstrapConfigConfirmationAuthJWTSigningKeyChange      = "auth-jwt-signing-key-change"
	BootstrapConfigConfirmationStateTransferBundleKeyChange = "state-transfer-bundle-encryption-key-change"
)

type BootstrapConfigSecretAction string

const (
	BootstrapConfigSecretActionPreserve BootstrapConfigSecretAction = "preserve"
	BootstrapConfigSecretActionReplace  BootstrapConfigSecretAction = "replace"
	BootstrapConfigSecretActionClear    BootstrapConfigSecretAction = "clear"
)

type BootstrapConfigSnapshot struct {
	ConfigPath    string                                   `json:"config_path"`
	SchemaVersion int                                      `json:"schema_version"`
	FileRevision  int                                      `json:"file_revision"`
	CreatedAt     string                                   `json:"created_at"`
	UpdatedAt     string                                   `json:"updated_at"`
	DocumentETag  string                                   `json:"document_etag"`
	Values        BootstrapConfigValues                    `json:"values"`
	Secrets       map[string]BootstrapConfigSecretMetadata `json:"secrets"`
}

type BootstrapConfigResponse struct {
	ConfigPath         string                                    `json:"config_path"`
	SchemaVersion      int                                       `json:"schema_version"`
	FileRevision       int                                       `json:"file_revision"`
	LoadedRevision     int                                       `json:"loaded_revision"`
	DocumentETag       string                                    `json:"document_etag"`
	LoadedDocumentETag string                                    `json:"loaded_document_etag"`
	CreatedAt          string                                    `json:"created_at"`
	UpdatedAt          string                                    `json:"updated_at"`
	RestartRequired    bool                                      `json:"restart_required"`
	Writable           bool                                      `json:"writable"`
	ApplyCapabilities  map[string]BootstrapConfigFieldCapability `json:"apply_capabilities"`
	PlannedChanges     *BootstrapConfigPlannedChanges            `json:"planned_changes,omitempty"`
	ApplyResult        *BootstrapConfigApplyResult               `json:"apply_result,omitempty"`
	Values             BootstrapConfigValues                     `json:"values"`
	Secrets            map[string]BootstrapConfigSecretMetadata  `json:"secrets"`
}

type BootstrapConfigPlannedChanges struct {
	ChangedFields   []BootstrapConfigFieldChange `json:"changed_fields"`
	RestartRequired bool                         `json:"restart_required"`
}

type BootstrapConfigApplyResult struct {
	AppliedNowFields      []string `json:"applied_now_fields"`
	RestartRequiredFields []string `json:"restart_required_fields"`
	UnchangedFields       []string `json:"unchanged_fields"`
	PendingHotApplyFields []string `json:"pending_hot_apply_fields"`
	FailedHotApplyFields  []string `json:"failed_hot_apply_fields"`
}

type BootstrapConfigResponseOptions struct {
	PlannedChanges *BootstrapConfigPlannedChanges
	ApplyResult    *BootstrapConfigApplyResult
}

type BootstrapConfigValues struct {
	Server    *BootstrapConfigServerValues    `json:"server"`
	Database  *BootstrapConfigDatabaseValues  `json:"database"`
	Runtime   *BootstrapConfigRuntimeValues   `json:"runtime"`
	HTTP      *BootstrapConfigHTTPValues      `json:"http"`
	Auth      *BootstrapConfigAuthValues      `json:"auth"`
	Mail      *BootstrapConfigMailValues      `json:"mail,omitempty"`
	Telemetry *BootstrapConfigTelemetryValues `json:"telemetry,omitempty"`
}

type BootstrapConfigServerValues struct {
	Host *string `json:"host"`
	Port *int    `json:"port"`
}

type BootstrapConfigDatabaseValues struct {
	Pools               *BootstrapConfigDatabasePoolsValues       `json:"pools"`
	ManagementAdmission *BootstrapConfigManagementAdmissionValues `json:"management_admission"`
}

type BootstrapConfigDatabasePoolsValues struct {
	TotalMaxConns    *int                               `json:"total_max_conns"`
	Management       *BootstrapConfigDatabasePoolValues `json:"management"`
	RuntimeExecution *BootstrapConfigDatabasePoolValues `json:"runtime_execution"`
	RuntimeTelemetry *BootstrapConfigDatabasePoolValues `json:"runtime_telemetry"`
	RuntimeFeedback  *BootstrapConfigDatabasePoolValues `json:"runtime_feedback"`
	Realtime         *BootstrapConfigDatabasePoolValues `json:"realtime"`
	CacheRefresh     *BootstrapConfigDatabasePoolValues `json:"cache_refresh"`
	BackgroundJobs   *BootstrapConfigDatabasePoolValues `json:"background_jobs"`
}

type BootstrapConfigDatabasePoolValues struct {
	MaxConns     *int `json:"max_conns"`
	MinIdleConns *int `json:"min_idle_conns"`
}

type BootstrapConfigManagementAdmissionValues struct {
	M2MaxConcurrent *int `json:"m2_max_concurrent"`
	M3MaxConcurrent *int `json:"m3_max_concurrent"`
}

type BootstrapConfigRuntimeValues struct {
	Transport   *BootstrapConfigRuntimeTransportValues   `json:"transport"`
	SideEffects *BootstrapConfigRuntimeSideEffectsValues `json:"side_effects"`
	Routing     *BootstrapConfigRuntimeRoutingValues     `json:"routing"`
}

type BootstrapConfigRuntimeTransportValues struct {
	MaxIdleConns          *int    `json:"max_idle_conns"`
	MaxIdleConnsPerHost   *int    `json:"max_idle_conns_per_host"`
	MaxConnsPerHost       *int    `json:"max_conns_per_host"`
	RequestTimeout        *string `json:"request_timeout"`
	IdleConnTimeout       *string `json:"idle_conn_timeout"`
	ResponseHeaderTimeout *string `json:"response_header_timeout"`
	TLSHandshakeTimeout   *string `json:"tls_handshake_timeout"`
	ExpectContinueTimeout *string `json:"expect_continue_timeout"`
}

type BootstrapConfigRuntimeSideEffectsValues struct {
	AttemptTimeout *string `json:"attempt_timeout"`
}

type BootstrapConfigRuntimeRoutingValues struct{}

type BootstrapConfigHTTPValues struct {
	CORSAllowedOrigins *[]string `json:"cors_allowed_origins"`
}

type BootstrapConfigAuthValues struct {
	AccessTokenTTLSeconds  *int    `json:"access_token_ttl_seconds"`
	RefreshTokenTTLSeconds *int    `json:"refresh_token_ttl_seconds"`
	ResetCodeTTLSeconds    *int    `json:"reset_code_ttl_seconds"`
	AccessCookieName       *string `json:"access_cookie_name"`
	RefreshCookieName      *string `json:"refresh_cookie_name"`
	CookieSecure           *bool   `json:"cookie_secure"`
}

type BootstrapConfigMailValues struct {
	Enabled *bool                          `json:"enabled"`
	From    *string                        `json:"from"`
	ReplyTo *string                        `json:"reply_to"`
	SMTP    *BootstrapConfigMailSMTPValues `json:"smtp"`
}

type BootstrapConfigMailSMTPValues struct {
	Host          *string `json:"host"`
	Port          *int    `json:"port"`
	Mode          *string `json:"mode"`
	EHLOHostname  *string `json:"ehlo_hostname"`
	Auth          *string `json:"auth"`
	Username      *string `json:"username"`
	PasswordFile  *string `json:"password_file"`
	Timeout       *string `json:"timeout"`
	TLSServerName *string `json:"tls_server_name"`
}

type BootstrapConfigTelemetryValues struct {
	Enabled  *bool                                   `json:"enabled"`
	Exporter *BootstrapConfigTelemetryExporterValues `json:"exporter,omitempty"`
	Metrics  *BootstrapConfigTelemetrySignalValues   `json:"metrics,omitempty"`
	Traces   *BootstrapConfigTelemetryTracesValues   `json:"traces,omitempty"`
}

type BootstrapConfigTelemetryExporterValues struct {
	Endpoint    *string                                     `json:"endpoint,omitempty"`
	Protocol    *string                                     `json:"protocol,omitempty"`
	Compression *string                                     `json:"compression,omitempty"`
	Timeout     *string                                     `json:"timeout,omitempty"`
	Auth        *BootstrapConfigTelemetryExporterAuthValues `json:"auth,omitempty"`
	TLS         *BootstrapConfigTelemetryExporterTLSValues  `json:"tls,omitempty"`
}

type BootstrapConfigTelemetryExporterAuthValues struct {
	Mode *string `json:"mode,omitempty"`
}

type BootstrapConfigTelemetryExporterTLSValues struct {
	InsecureSkipVerify *bool   `json:"insecure_skip_verify"`
	CAFile             *string `json:"ca_file,omitempty"`
}

type BootstrapConfigTelemetrySignalValues struct {
	Enabled *bool `json:"enabled"`
}

type BootstrapConfigTelemetryTracesValues struct {
	Enabled       *bool    `json:"enabled"`
	SamplingRatio *float64 `json:"sampling_ratio,omitempty"`
}

type BootstrapConfigSecretMetadata struct {
	Configured bool   `json:"configured"`
	Editable   bool   `json:"editable"`
	Masked     string `json:"masked"`
}

type BootstrapConfigSecretUpdate struct {
	Action BootstrapConfigSecretAction `json:"action"`
	Value  *string                     `json:"value,omitempty"`
}

type BootstrapConfigUpdateRequest struct {
	ExpectedRevision int                                    `json:"expected_revision"`
	ExpectedETag     string                                 `json:"expected_etag"`
	Values           *BootstrapConfigValues                 `json:"values"`
	SecretUpdates    map[string]BootstrapConfigSecretUpdate `json:"secret_updates"`
	Confirmations    []string                               `json:"confirmations,omitempty"`
}

type BootstrapConfigPreparedUpdate struct {
	Payload  []byte                  `json:"-"`
	Snapshot BootstrapConfigSnapshot `json:"snapshot"`
	Noop     bool                    `json:"noop"`
}

type BootstrapConfigConflictError struct {
	ExpectedRevision int
	CurrentRevision  int
	ExpectedETag     string
	CurrentETag      string
}

func (e *BootstrapConfigConflictError) Error() string {
	return "bootstrap config has changed since it was loaded"
}

type BootstrapConfigSecretOperationError struct {
	Field  string
	Action BootstrapConfigSecretAction
	Reason string
}

func (e *BootstrapConfigSecretOperationError) Error() string {
	if e.Action == "" {
		return fmt.Sprintf("bootstrap config secret %s is invalid: %s", e.Field, e.Reason)
	}
	return fmt.Sprintf("bootstrap config secret %s action %q is invalid: %s", e.Field, e.Action, e.Reason)
}

type BootstrapConfigMissingConfirmationsError struct {
	RequiredConfirmations []string
}

func (e *BootstrapConfigMissingConfirmationsError) Error() string {
	return fmt.Sprintf("bootstrap config update requires confirmations: %s", strings.Join(e.RequiredConfirmations, ", "))
}

type bootstrapConfigDocument struct {
	Meta          *bootstrapMeta          `json:"meta"`
	Server        *bootstrapServer        `json:"server"`
	Database      *bootstrapDatabase      `json:"database"`
	Runtime       *bootstrapRuntime       `json:"runtime"`
	HTTP          *bootstrapHTTP          `json:"http"`
	Auth          *bootstrapAuth          `json:"auth"`
	Mail          *bootstrapMail          `json:"mail,omitempty"`
	Telemetry     *bootstrapTelemetry     `json:"telemetry,omitempty"`
	StateTransfer *bootstrapStateTransfer `json:"stateTransfer"`
}

type bootstrapMeta struct {
	SchemaVersion *int    `json:"schemaVersion"`
	Revision      *int    `json:"revision"`
	CreatedAt     *string `json:"createdAt"`
	UpdatedAt     *string `json:"updatedAt"`
}

type bootstrapServer struct {
	Host *string `json:"host"`
	Port *int    `json:"port"`
}

type bootstrapDatabase struct {
	URL                 *string                       `json:"url"`
	Pools               *bootstrapDatabasePools       `json:"pools"`
	ManagementAdmission *bootstrapManagementAdmission `json:"managementAdmission"`
}

type bootstrapDatabasePools struct {
	TotalMaxConns    *int                   `json:"totalMaxConns"`
	Management       *bootstrapDatabasePool `json:"management"`
	RuntimeExecution *bootstrapDatabasePool `json:"runtimeExecution"`
	RuntimeTelemetry *bootstrapDatabasePool `json:"runtimeTelemetry"`
	RuntimeFeedback  *bootstrapDatabasePool `json:"runtimeFeedback"`
	Realtime         *bootstrapDatabasePool `json:"realtime"`
	CacheRefresh     *bootstrapDatabasePool `json:"cacheRefresh"`
	BackgroundJobs   *bootstrapDatabasePool `json:"backgroundJobs"`
}

type bootstrapDatabasePool struct {
	MaxConns     *int `json:"maxConns"`
	MinIdleConns *int `json:"minIdleConns"`
}

type bootstrapManagementAdmission struct {
	M2MaxConcurrent *int `json:"m2MaxConcurrent"`
	M3MaxConcurrent *int `json:"m3MaxConcurrent"`
}

type bootstrapRuntime struct {
	SecretEncryptionKey *string                      `json:"secretEncryptionKey"`
	Transport           *bootstrapRuntimeTransport   `json:"transport"`
	SideEffects         *bootstrapRuntimeSideEffects `json:"sideEffects"`
	Routing             *bootstrapRuntimeRouting     `json:"routing,omitempty"`
}

type bootstrapRuntimeTransport struct {
	MaxIdleConns          *int    `json:"maxIdleConns"`
	MaxIdleConnsPerHost   *int    `json:"maxIdleConnsPerHost"`
	MaxConnsPerHost       *int    `json:"maxConnsPerHost"`
	RequestTimeout        *string `json:"requestTimeout"`
	IdleConnTimeout       *string `json:"idleConnTimeout"`
	ResponseHeaderTimeout *string `json:"responseHeaderTimeout"`
	TLSHandshakeTimeout   *string `json:"tlsHandshakeTimeout"`
	ExpectContinueTimeout *string `json:"expectContinueTimeout"`
}

type bootstrapRuntimeSideEffects struct {
	AttemptTimeout *string `json:"attemptTimeout"`
}

type bootstrapRuntimeRouting struct{}

type bootstrapHTTP struct {
	CORSAllowedOrigins *[]string `json:"corsAllowedOrigins"`
}

type bootstrapAuth struct {
	JWTSigningKey          *string `json:"jwtSigningKey"`
	AccessTokenTTLSeconds  *int    `json:"accessTokenTtlSeconds"`
	RefreshTokenTTLSeconds *int    `json:"refreshTokenTtlSeconds"`
	ResetCodeTTLSeconds    *int    `json:"resetCodeTtlSeconds"`
	AccessCookieName       *string `json:"accessCookieName"`
	RefreshCookieName      *string `json:"refreshCookieName"`
	CookieSecure           *bool   `json:"cookieSecure"`
}

type bootstrapMail struct {
	Enabled *bool          `json:"enabled"`
	From    *string        `json:"from,omitempty"`
	ReplyTo *string        `json:"replyTo,omitempty"`
	SMTP    *bootstrapSMTP `json:"smtp,omitempty"`
}

type bootstrapSMTP struct {
	Host          *string `json:"host,omitempty"`
	Port          *int    `json:"port,omitempty"`
	Mode          *string `json:"mode,omitempty"`
	EHLOHostname  *string `json:"ehloHostname,omitempty"`
	Auth          *string `json:"auth,omitempty"`
	Username      *string `json:"username,omitempty"`
	Password      *string `json:"password,omitempty"`
	PasswordFile  *string `json:"passwordFile,omitempty"`
	Timeout       *string `json:"timeout,omitempty"`
	TLSServerName *string `json:"tlsServerName,omitempty"`
}

type bootstrapTelemetry struct {
	Enabled  *bool                       `json:"enabled"`
	Exporter *bootstrapTelemetryExporter `json:"exporter,omitempty"`
	Metrics  *bootstrapTelemetrySignal   `json:"metrics,omitempty"`
	Traces   *bootstrapTelemetryTraces   `json:"traces,omitempty"`
}

type bootstrapTelemetryExporter struct {
	Endpoint    *string                         `json:"endpoint,omitempty"`
	Protocol    *string                         `json:"protocol,omitempty"`
	Compression *string                         `json:"compression,omitempty"`
	Timeout     *string                         `json:"timeout,omitempty"`
	Auth        *bootstrapTelemetryExporterAuth `json:"auth,omitempty"`
	TLS         *bootstrapTelemetryExporterTLS  `json:"tls,omitempty"`
}

type bootstrapTelemetryExporterAuth struct {
	Mode                *string `json:"mode,omitempty"`
	AuthorizationHeader *string `json:"authorizationHeader,omitempty"`
}

type bootstrapTelemetryExporterTLS struct {
	InsecureSkipVerify *bool   `json:"insecureSkipVerify,omitempty"`
	CAFile             *string `json:"caFile,omitempty"`
}

type bootstrapTelemetrySignal struct {
	Enabled *bool `json:"enabled"`
}

type bootstrapTelemetryTraces struct {
	Enabled       *bool    `json:"enabled"`
	SamplingRatio *float64 `json:"samplingRatio,omitempty"`
}

type bootstrapStateTransfer struct {
	BundleEncryptionKey *string `json:"bundleEncryptionKey"`
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

func BuildBootstrapConfigResponse(snapshot BootstrapConfigSnapshot, currentSettings Settings, liveSettings Settings, loadedRevision int, loadedDocumentETag string, writable bool, options BootstrapConfigResponseOptions) BootstrapConfigResponse {
	loadedETag := strings.TrimSpace(loadedDocumentETag)
	fieldDiff, err := DiffBootstrapConfigSettings(liveSettings, currentSettings)
	restartRequired := err == nil && fieldDiff.RestartRequired()
	if options.PlannedChanges != nil {
		restartRequired = options.PlannedChanges.RestartRequired
	}
	if options.ApplyResult != nil {
		restartRequired = len(options.ApplyResult.RestartRequiredFields) > 0
	}
	return BootstrapConfigResponse{
		ConfigPath:         snapshot.ConfigPath,
		SchemaVersion:      snapshot.SchemaVersion,
		FileRevision:       snapshot.FileRevision,
		LoadedRevision:     loadedRevision,
		DocumentETag:       snapshot.DocumentETag,
		LoadedDocumentETag: loadedETag,
		CreatedAt:          snapshot.CreatedAt,
		UpdatedAt:          snapshot.UpdatedAt,
		RestartRequired:    restartRequired,
		Writable:           writable,
		ApplyCapabilities:  BootstrapConfigApplyCapabilities(),
		PlannedChanges:     options.PlannedChanges,
		ApplyResult:        options.ApplyResult,
		Values:             snapshot.Values,
		Secrets:            snapshot.Secrets,
	}
}

func (m BootstrapConfigManager) PrepareBootstrapConfigUpdate(path string, request BootstrapConfigUpdateRequest) (BootstrapConfigPreparedUpdate, error) {
	currentDocument, currentSnapshot, _, currentPayload, err := m.loadBootstrapConfigDocument(path)
	if err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	if err := validateBootstrapConfigExpectations(currentSnapshot, request); err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}

	candidate := cloneBootstrapConfigDocument(currentDocument)
	applyBootstrapConfigValues(&candidate, request.Values)
	if err := applyBootstrapConfigSecretUpdates(&candidate, currentDocument, request.SecretUpdates); err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	if err := candidate.validateSchema(); err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	if err := candidate.validateSemantics(); err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	if _, err := candidate.toSettings(); err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	if requiredConfirmations := missingBootstrapConfigConfirmations(currentDocument, candidate, request.Confirmations); len(requiredConfirmations) > 0 {
		return BootstrapConfigPreparedUpdate{}, &BootstrapConfigMissingConfirmationsError{RequiredConfirmations: requiredConfirmations}
	}

	candidatePayload, err := canonicalBootstrapConfigPayload(candidate)
	if err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	if bytes.Equal(candidatePayload, currentPayload) {
		return BootstrapConfigPreparedUpdate{Payload: cloneBytes(currentPayload), Snapshot: currentSnapshot, Noop: true}, nil
	}

	revision, err := requiredIntMin("meta.revision", currentDocument.Meta.Revision, 1)
	if err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	candidate.Meta.Revision = intPointer(revision + 1)
	candidate.Meta.UpdatedAt = stringPointer(m.resolvedTimeNow()().UTC().Format(time.RFC3339))

	updatedSnapshot, _, updatedPayload, err := buildBootstrapConfigSnapshot(currentSnapshot.ConfigPath, candidate)
	if err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	return BootstrapConfigPreparedUpdate{Payload: cloneBytes(updatedPayload), Snapshot: updatedSnapshot}, nil
}

func (m BootstrapConfigManager) ValidateBootstrapConfigUpdate(path string, request BootstrapConfigUpdateRequest) (BootstrapConfigPreparedUpdate, error) {
	return m.PrepareBootstrapConfigUpdate(path, request)
}

func (m BootstrapConfigManager) WriteBootstrapConfigUpdate(path string, prepared BootstrapConfigPreparedUpdate) (BootstrapConfigSnapshot, error) {
	if prepared.Noop {
		return prepared.Snapshot, nil
	}
	if len(bytes.TrimSpace(prepared.Payload)) == 0 {
		return BootstrapConfigSnapshot{}, fmt.Errorf("bootstrap config prepared payload is empty")
	}
	if err := m.WriteAtomically(path, prepared.Payload); err != nil {
		return BootstrapConfigSnapshot{}, err
	}
	return prepared.Snapshot, nil
}

func (m BootstrapConfigManager) SaveBootstrapConfigUpdate(path string, request BootstrapConfigUpdateRequest) (BootstrapConfigPreparedUpdate, error) {
	prepared, err := m.PrepareBootstrapConfigUpdate(path, request)
	if err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	if _, err := m.WriteBootstrapConfigUpdate(path, prepared); err != nil {
		return BootstrapConfigPreparedUpdate{}, err
	}
	return prepared, nil
}

func (m BootstrapConfigManager) Parse(raw []byte) (Settings, error) {
	document, err := parseBootstrapConfigDocument(raw)
	if err != nil {
		return Settings{}, err
	}
	return document.toSettings()
}

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

func validateBootstrapConfigExpectations(current BootstrapConfigSnapshot, request BootstrapConfigUpdateRequest) error {
	expectedETag := strings.TrimSpace(request.ExpectedETag)
	if request.ExpectedRevision == current.FileRevision && expectedETag == current.DocumentETag {
		return nil
	}
	return &BootstrapConfigConflictError{
		ExpectedRevision: request.ExpectedRevision,
		CurrentRevision:  current.FileRevision,
		ExpectedETag:     expectedETag,
		CurrentETag:      current.DocumentETag,
	}
}

func applyBootstrapConfigValues(document *bootstrapConfigDocument, values *BootstrapConfigValues) {
	if values == nil {
		document.Server = nil
		document.Database = nil
		document.Runtime = nil
		document.HTTP = nil
		document.Auth = nil
		document.Mail = nil
		document.Telemetry = nil
		return
	}
	databaseURL := currentBootstrapDatabaseURL(document)
	runtimeSecret := currentBootstrapRuntimeSecret(document)
	authJWTSigningKey := currentBootstrapAuthJWTSigningKey(document)
	mailSMTPPassword := currentBootstrapMailSMTPPassword(document)
	telemetryAuthorizationHeader := currentBootstrapTelemetryAuthorizationHeader(document)
	telemetryWasOmitted := document.Telemetry == nil
	document.Server = bootstrapServerFromSafeValues(values.Server)
	document.Database = bootstrapDatabaseFromSafeValues(values.Database, databaseURL)
	document.Runtime = bootstrapRuntimeFromSafeValues(values.Runtime, runtimeSecret)
	document.HTTP = bootstrapHTTPFromSafeValues(values.HTTP)
	document.Auth = bootstrapAuthFromSafeValues(values.Auth, authJWTSigningKey)
	document.Mail = bootstrapMailFromSafeValues(values.Mail, mailSMTPPassword)
	document.Telemetry = bootstrapTelemetryFromSafeValues(values.Telemetry, telemetryAuthorizationHeader)
	if telemetryWasOmitted && isDisabledSafeBootstrapTelemetry(values.Telemetry) {
		document.Telemetry = nil
	}
}

func applyBootstrapConfigSecretUpdates(candidate *bootstrapConfigDocument, current bootstrapConfigDocument, updates map[string]BootstrapConfigSecretUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	unknownFields := make([]string, 0)
	for field := range updates {
		if !isKnownBootstrapConfigSecretField(field) {
			unknownFields = append(unknownFields, field)
		}
	}
	if len(unknownFields) > 0 {
		slices.Sort(unknownFields)
		return &BootstrapConfigSecretOperationError{Field: unknownFields[0], Reason: "unsupported secret field"}
	}
	for _, field := range orderedBootstrapConfigSecretFields() {
		update, ok := updates[field]
		if !ok {
			continue
		}
		switch update.Action {
		case BootstrapConfigSecretActionPreserve:
			continue
		case BootstrapConfigSecretActionReplace:
			if field == BootstrapConfigSecretRuntimeSecretEncryptionKey {
				return &BootstrapConfigSecretOperationError{Field: field, Action: update.Action, Reason: "runtime secret encryption key is preserve-only in v1"}
			}
			if field == BootstrapConfigSecretMailSMTPPassword && !bootstrapMailEnabled(candidate.Mail) {
				continue
			}
			if field == BootstrapConfigSecretTelemetryAuthorizationHeader && !bootstrapTelemetryAuthorizationHeaderEnabled(candidate.Telemetry) {
				continue
			}
			value, err := replacementBootstrapSecretValue(field, update.Value, current)
			if err != nil {
				return err
			}
			setBootstrapConfigSecret(candidate, field, value)
		case BootstrapConfigSecretActionClear:
			if field != BootstrapConfigSecretTelemetryAuthorizationHeader {
				return &BootstrapConfigSecretOperationError{Field: field, Action: update.Action, Reason: "clear is only supported for telemetry authorization header"}
			}
			clearBootstrapConfigSecret(candidate, field)
		default:
			return &BootstrapConfigSecretOperationError{Field: field, Action: update.Action, Reason: "action must be preserve, replace, or clear"}
		}
	}
	return nil
}

func replacementBootstrapSecretValue(field string, value *string, current bootstrapConfigDocument) (string, error) {
	if value == nil {
		return "", &BootstrapConfigSecretOperationError{Field: field, Action: BootstrapConfigSecretActionReplace, Reason: "replacement value is required"}
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return "", &BootstrapConfigSecretOperationError{Field: field, Action: BootstrapConfigSecretActionReplace, Reason: "replacement value is required"}
	}
	metadata := bootstrapConfigSecretMetadata(current)[field]
	if isRedactedBootstrapSecretPlaceholder(trimmed, metadata) || databaseURLHasRedactedQueryPlaceholder(field, trimmed) {
		return "", &BootstrapConfigSecretOperationError{Field: field, Action: BootstrapConfigSecretActionReplace, Reason: "replacement value must not be a redacted placeholder"}
	}
	return trimmed, nil
}

func isRedactedBootstrapSecretPlaceholder(value string, metadata BootstrapConfigSecretMetadata) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if metadata.Masked != "" && trimmed == metadata.Masked {
		return true
	}
	if isBootstrapRedactedToken(trimmed) {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(trimmed, ":***@") || strings.Contains(lower, "%2a%2a%2a") || strings.Contains(lower, "[redacted]")
}

func databaseURLHasRedactedQueryPlaceholder(field string, value string) bool {
	if field != BootstrapConfigSecretDatabaseURL {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	query := parsed.Query()
	for key, values := range query {
		if !isSensitiveDatabaseURLQueryKey(key) {
			continue
		}
		for _, item := range values {
			if isBootstrapRedactedToken(item) {
				return true
			}
		}
	}
	return false
}

func isBootstrapRedactedToken(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch lower {
	case "***", "set", "configured", "redacted", "[redacted]", "********":
		return true
	default:
		return strings.Contains(lower, "%2a%2a%2a") || strings.Contains(lower, "[redacted]")
	}
}

func setBootstrapConfigSecret(document *bootstrapConfigDocument, field string, value string) {
	switch field {
	case BootstrapConfigSecretDatabaseURL:
		if document.Database == nil {
			document.Database = &bootstrapDatabase{}
		}
		document.Database.URL = stringPointer(value)
	case BootstrapConfigSecretAuthJWTSigningKey:
		if document.Auth == nil {
			document.Auth = &bootstrapAuth{}
		}
		document.Auth.JWTSigningKey = stringPointer(value)
	case BootstrapConfigSecretStateTransferBundleKey:
		if document.StateTransfer == nil {
			document.StateTransfer = &bootstrapStateTransfer{}
		}
		document.StateTransfer.BundleEncryptionKey = stringPointer(value)
	case BootstrapConfigSecretMailSMTPPassword:
		if document.Mail == nil {
			document.Mail = &bootstrapMail{}
		}
		if document.Mail.SMTP == nil {
			document.Mail.SMTP = &bootstrapSMTP{}
		}
		document.Mail.SMTP.Password = stringPointer(value)
	case BootstrapConfigSecretTelemetryAuthorizationHeader:
		if document.Telemetry == nil {
			document.Telemetry = &bootstrapTelemetry{}
		}
		if document.Telemetry.Exporter == nil {
			document.Telemetry.Exporter = &bootstrapTelemetryExporter{}
		}
		if document.Telemetry.Exporter.Auth == nil {
			document.Telemetry.Exporter.Auth = &bootstrapTelemetryExporterAuth{}
		}
		document.Telemetry.Exporter.Auth.AuthorizationHeader = stringPointer(value)
	}
}

func clearBootstrapConfigSecret(document *bootstrapConfigDocument, field string) {
	switch field {
	case BootstrapConfigSecretTelemetryAuthorizationHeader:
		if document.Telemetry == nil || document.Telemetry.Exporter == nil || document.Telemetry.Exporter.Auth == nil {
			return
		}
		document.Telemetry.Exporter.Auth.AuthorizationHeader = nil
	}
}

func missingBootstrapConfigConfirmations(current bootstrapConfigDocument, candidate bootstrapConfigDocument, confirmations []string) []string {
	provided := make(map[string]struct{}, len(confirmations))
	for _, confirmation := range confirmations {
		trimmed := strings.TrimSpace(confirmation)
		if trimmed != "" {
			provided[trimmed] = struct{}{}
		}
	}
	required := make([]string, 0, 5)
	if bootstrapStringValueChanged(current.Server.Host, candidate.Server.Host) {
		required = append(required, BootstrapConfigConfirmationServerHostChange)
	}
	if bootstrapIntValueChanged(current.Server.Port, candidate.Server.Port) {
		required = append(required, BootstrapConfigConfirmationServerPortChange)
	}
	if bootstrapStringValueChanged(current.Database.URL, candidate.Database.URL) {
		required = append(required, BootstrapConfigConfirmationDatabaseURLChange)
	}
	if bootstrapStringValueChanged(current.Auth.JWTSigningKey, candidate.Auth.JWTSigningKey) {
		required = append(required, BootstrapConfigConfirmationAuthJWTSigningKeyChange)
	}
	if bootstrapStringValueChanged(current.StateTransfer.BundleEncryptionKey, candidate.StateTransfer.BundleEncryptionKey) {
		required = append(required, BootstrapConfigConfirmationStateTransferBundleKeyChange)
	}
	missing := make([]string, 0, len(required))
	for _, confirmation := range required {
		if _, ok := provided[confirmation]; !ok {
			missing = append(missing, confirmation)
		}
	}
	return missing
}

func safeBootstrapConfigValues(document bootstrapConfigDocument) BootstrapConfigValues {
	return BootstrapConfigValues{
		Server: &BootstrapConfigServerValues{
			Host: cloneStringPointer(document.Server.Host),
			Port: cloneIntPointer(document.Server.Port),
		},
		Database: &BootstrapConfigDatabaseValues{
			Pools: safeBootstrapDatabasePoolsValues(document.Database.Pools),
			ManagementAdmission: &BootstrapConfigManagementAdmissionValues{
				M2MaxConcurrent: cloneIntPointer(document.Database.ManagementAdmission.M2MaxConcurrent),
				M3MaxConcurrent: cloneIntPointer(document.Database.ManagementAdmission.M3MaxConcurrent),
			},
		},
		Runtime: &BootstrapConfigRuntimeValues{
			Transport:   safeBootstrapRuntimeTransportValues(document.Runtime.Transport),
			SideEffects: safeBootstrapRuntimeSideEffectsValues(document.Runtime.SideEffects),
			Routing:     safeBootstrapRuntimeRoutingValues(document.Runtime.Routing),
		},
		HTTP: &BootstrapConfigHTTPValues{
			CORSAllowedOrigins: cloneStringSlicePointer(document.HTTP.CORSAllowedOrigins),
		},
		Auth: &BootstrapConfigAuthValues{
			AccessTokenTTLSeconds:  cloneIntPointer(document.Auth.AccessTokenTTLSeconds),
			RefreshTokenTTLSeconds: cloneIntPointer(document.Auth.RefreshTokenTTLSeconds),
			ResetCodeTTLSeconds:    cloneIntPointer(document.Auth.ResetCodeTTLSeconds),
			AccessCookieName:       cloneStringPointer(document.Auth.AccessCookieName),
			RefreshCookieName:      cloneStringPointer(document.Auth.RefreshCookieName),
			CookieSecure:           cloneBoolPointer(document.Auth.CookieSecure),
		},
		Mail:      safeBootstrapMailValues(document.Mail),
		Telemetry: safeBootstrapTelemetryValues(document.Telemetry),
	}
}

func bootstrapConfigSecretMetadata(document bootstrapConfigDocument) map[string]BootstrapConfigSecretMetadata {
	return map[string]BootstrapConfigSecretMetadata{
		BootstrapConfigSecretDatabaseURL:                  secretMetadata(document.Database.URL, true, maskBootstrapDatabaseURL),
		BootstrapConfigSecretRuntimeSecretEncryptionKey:   secretMetadata(document.Runtime.SecretEncryptionKey, false, maskConfiguredBootstrapSecret),
		BootstrapConfigSecretAuthJWTSigningKey:            secretMetadata(document.Auth.JWTSigningKey, true, maskConfiguredBootstrapSecret),
		BootstrapConfigSecretStateTransferBundleKey:       secretMetadata(document.StateTransfer.BundleEncryptionKey, true, maskConfiguredBootstrapSecret),
		BootstrapConfigSecretMailSMTPPassword:             secretMetadata(bootstrapMailSMTPPassword(document.Mail), true, maskConfiguredBootstrapSecret),
		BootstrapConfigSecretTelemetryAuthorizationHeader: secretMetadata(bootstrapTelemetryAuthorizationHeader(document.Telemetry), true, maskConfiguredBootstrapSecret),
	}
}

func secretMetadata(value *string, editable bool, mask func(string) string) BootstrapConfigSecretMetadata {
	if value == nil || strings.TrimSpace(*value) == "" {
		return BootstrapConfigSecretMetadata{Editable: editable}
	}
	return BootstrapConfigSecretMetadata{Configured: true, Editable: editable, Masked: mask(*value)}
}

func maskConfiguredBootstrapSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "set"
}

func maskBootstrapDatabaseURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "set"
	}
	if parsed.User != nil {
		username := parsed.User.Username()
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(username, "***")
		}
	}
	query := parsed.Query()
	for key, values := range query {
		if !isSensitiveDatabaseURLQueryKey(key) {
			continue
		}
		for index := range values {
			values[index] = "***"
		}
		query[key] = values
	}
	parsed.RawQuery = query.Encode()
	masked := parsed.String()
	masked = strings.ReplaceAll(masked, "%2A%2A%2A", "***")
	masked = strings.ReplaceAll(masked, "%2a%2a%2a", "***")
	return masked
}

func isSensitiveDatabaseURLQueryKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return lower == "pass" || lower == "pwd" || lower == "passwd" || strings.Contains(lower, "password") || strings.Contains(lower, "passphrase") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "key")
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
		Transport:           bootstrapRuntimeTransportFromSafeValues(values.Transport),
		SideEffects:         bootstrapRuntimeSideEffectsFromSafeValues(values.SideEffects),
		Routing:             bootstrapRuntimeRoutingFromSafeValues(values.Routing),
	}
}

func bootstrapRuntimeTransportFromSafeValues(values *BootstrapConfigRuntimeTransportValues) *bootstrapRuntimeTransport {
	if values == nil {
		return nil
	}
	return &bootstrapRuntimeTransport{
		MaxIdleConns:          cloneIntPointer(values.MaxIdleConns),
		MaxIdleConnsPerHost:   cloneIntPointer(values.MaxIdleConnsPerHost),
		MaxConnsPerHost:       cloneIntPointer(values.MaxConnsPerHost),
		RequestTimeout:        cloneStringPointer(values.RequestTimeout),
		IdleConnTimeout:       cloneStringPointer(values.IdleConnTimeout),
		ResponseHeaderTimeout: cloneStringPointer(values.ResponseHeaderTimeout),
		TLSHandshakeTimeout:   cloneStringPointer(values.TLSHandshakeTimeout),
		ExpectContinueTimeout: cloneStringPointer(values.ExpectContinueTimeout),
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

func bootstrapMailFromSafeValues(values *BootstrapConfigMailValues, smtpPassword *string) *bootstrapMail {
	if values == nil || values.Enabled == nil || !*values.Enabled {
		return canonicalDisabledBootstrapMailDocument()
	}
	return &bootstrapMail{
		Enabled: cloneBoolPointer(values.Enabled),
		From:    cloneStringPointer(values.From),
		ReplyTo: cloneStringPointer(values.ReplyTo),
		SMTP:    bootstrapSMTPFromSafeValues(values.SMTP, smtpPassword),
	}
}

func bootstrapSMTPFromSafeValues(values *BootstrapConfigMailSMTPValues, smtpPassword *string) *bootstrapSMTP {
	if values == nil {
		return nil
	}
	preservedPassword := cloneStringPointer(smtpPassword)
	if hasNonEmptyString(values.PasswordFile) {
		preservedPassword = nil
	}
	return &bootstrapSMTP{
		Host:          cloneStringPointer(values.Host),
		Port:          cloneIntPointer(values.Port),
		Mode:          cloneStringPointer(values.Mode),
		EHLOHostname:  cloneStringPointer(values.EHLOHostname),
		Auth:          cloneStringPointer(values.Auth),
		Username:      cloneStringPointer(values.Username),
		Password:      preservedPassword,
		PasswordFile:  cloneStringPointer(values.PasswordFile),
		Timeout:       cloneStringPointer(values.Timeout),
		TLSServerName: cloneStringPointer(values.TLSServerName),
	}
}

func bootstrapTelemetryFromSafeValues(values *BootstrapConfigTelemetryValues, authorizationHeader *string) *bootstrapTelemetry {
	if isDisabledSafeBootstrapTelemetry(values) {
		return canonicalDisabledBootstrapTelemetryDocument()
	}
	return &bootstrapTelemetry{
		Enabled:  cloneBoolPointer(values.Enabled),
		Exporter: bootstrapTelemetryExporterFromSafeValues(values.Exporter, authorizationHeader),
		Metrics:  bootstrapTelemetrySignalFromSafeValues(values.Metrics),
		Traces:   bootstrapTelemetryTracesFromSafeValues(values.Traces),
	}
}

func isDisabledSafeBootstrapTelemetry(values *BootstrapConfigTelemetryValues) bool {
	return values == nil || values.Enabled == nil || !*values.Enabled
}

func bootstrapTelemetryExporterFromSafeValues(values *BootstrapConfigTelemetryExporterValues, authorizationHeader *string) *bootstrapTelemetryExporter {
	if values == nil {
		return nil
	}
	return &bootstrapTelemetryExporter{
		Endpoint:    cloneStringPointer(values.Endpoint),
		Protocol:    cloneStringPointer(values.Protocol),
		Compression: cloneStringPointer(values.Compression),
		Timeout:     cloneStringPointer(values.Timeout),
		Auth:        bootstrapTelemetryExporterAuthFromSafeValues(values.Auth, authorizationHeader),
		TLS:         bootstrapTelemetryExporterTLSFromSafeValues(values.TLS),
	}
}

func bootstrapTelemetryExporterAuthFromSafeValues(values *BootstrapConfigTelemetryExporterAuthValues, authorizationHeader *string) *bootstrapTelemetryExporterAuth {
	if values == nil {
		return nil
	}
	preservedHeader := cloneStringPointer(authorizationHeader)
	if values.Mode == nil || strings.TrimSpace(*values.Mode) != string(TelemetryExporterAuthModeAuthorizationHeader) {
		preservedHeader = nil
	}
	return &bootstrapTelemetryExporterAuth{
		Mode:                cloneStringPointer(values.Mode),
		AuthorizationHeader: preservedHeader,
	}
}

func bootstrapTelemetryExporterTLSFromSafeValues(values *BootstrapConfigTelemetryExporterTLSValues) *bootstrapTelemetryExporterTLS {
	if values == nil {
		return nil
	}
	return &bootstrapTelemetryExporterTLS{
		InsecureSkipVerify: cloneBoolPointer(values.InsecureSkipVerify),
		CAFile:             cloneStringPointer(values.CAFile),
	}
}

func bootstrapTelemetrySignalFromSafeValues(values *BootstrapConfigTelemetrySignalValues) *bootstrapTelemetrySignal {
	if values == nil {
		return nil
	}
	return &bootstrapTelemetrySignal{Enabled: cloneBoolPointer(values.Enabled)}
}

func bootstrapTelemetryTracesFromSafeValues(values *BootstrapConfigTelemetryTracesValues) *bootstrapTelemetryTraces {
	if values == nil {
		return nil
	}
	return &bootstrapTelemetryTraces{Enabled: cloneBoolPointer(values.Enabled), SamplingRatio: cloneFloat64Pointer(values.SamplingRatio)}
}

func currentBootstrapDatabaseURL(document *bootstrapConfigDocument) *string {
	if document == nil || document.Database == nil {
		return nil
	}
	return cloneStringPointer(document.Database.URL)
}

func currentBootstrapRuntimeSecret(document *bootstrapConfigDocument) *string {
	if document == nil || document.Runtime == nil {
		return nil
	}
	return cloneStringPointer(document.Runtime.SecretEncryptionKey)
}

func currentBootstrapAuthJWTSigningKey(document *bootstrapConfigDocument) *string {
	if document == nil || document.Auth == nil {
		return nil
	}
	return cloneStringPointer(document.Auth.JWTSigningKey)
}

func currentBootstrapMailSMTPPassword(document *bootstrapConfigDocument) *string {
	if document == nil {
		return nil
	}
	return cloneStringPointer(bootstrapMailSMTPPassword(document.Mail))
}

func currentBootstrapTelemetryAuthorizationHeader(document *bootstrapConfigDocument) *string {
	if document == nil {
		return nil
	}
	return cloneStringPointer(bootstrapTelemetryAuthorizationHeader(document.Telemetry))
}

func bootstrapMailSMTPPassword(mailConfig *bootstrapMail) *string {
	if mailConfig == nil || mailConfig.SMTP == nil {
		return nil
	}
	return mailConfig.SMTP.Password
}

func bootstrapMailEnabled(mailConfig *bootstrapMail) bool {
	return mailConfig != nil && mailConfig.Enabled != nil && *mailConfig.Enabled
}

func bootstrapTelemetryAuthorizationHeader(telemetry *bootstrapTelemetry) *string {
	if telemetry == nil || telemetry.Exporter == nil || telemetry.Exporter.Auth == nil {
		return nil
	}
	return telemetry.Exporter.Auth.AuthorizationHeader
}

func bootstrapTelemetryAuthorizationHeaderEnabled(telemetry *bootstrapTelemetry) bool {
	if telemetry == nil || telemetry.Exporter == nil || telemetry.Exporter.Auth == nil || telemetry.Exporter.Auth.Mode == nil {
		return false
	}
	return strings.TrimSpace(*telemetry.Exporter.Auth.Mode) == string(TelemetryExporterAuthModeAuthorizationHeader)
}

func orderedBootstrapConfigSecretFields() []string {
	return []string{
		BootstrapConfigSecretDatabaseURL,
		BootstrapConfigSecretRuntimeSecretEncryptionKey,
		BootstrapConfigSecretAuthJWTSigningKey,
		BootstrapConfigSecretStateTransferBundleKey,
		BootstrapConfigSecretMailSMTPPassword,
		BootstrapConfigSecretTelemetryAuthorizationHeader,
	}
}

func isKnownBootstrapConfigSecretField(field string) bool {
	switch field {
	case BootstrapConfigSecretDatabaseURL, BootstrapConfigSecretRuntimeSecretEncryptionKey, BootstrapConfigSecretAuthJWTSigningKey, BootstrapConfigSecretStateTransferBundleKey, BootstrapConfigSecretMailSMTPPassword, BootstrapConfigSecretTelemetryAuthorizationHeader:
		return true
	default:
		return false
	}
}

func bootstrapStringValueChanged(current *string, candidate *string) bool {
	if current == nil || candidate == nil {
		return false
	}
	return strings.TrimSpace(*current) != strings.TrimSpace(*candidate)
}

func bootstrapIntValueChanged(current *int, candidate *int) bool {
	if current == nil || candidate == nil {
		return false
	}
	return *current != *candidate
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

func canonicalBootstrapConfigPayload(document bootstrapConfigDocument) ([]byte, error) {
	payload, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal canonical bootstrap config JSON: %w", err)
	}
	return payload, nil
}

func bootstrapConfigETag(payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("sha256:%x", sum[:])
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
	clone.Mail = bootstrapMailFromSafeValues(safeBootstrapMailValues(document.Mail), currentBootstrapMailSMTPPassword(&document))
	clone.Telemetry = cloneBootstrapTelemetry(document.Telemetry)
	if document.StateTransfer != nil {
		clone.StateTransfer = &bootstrapStateTransfer{BundleEncryptionKey: cloneStringPointer(document.StateTransfer.BundleEncryptionKey)}
	}
	return clone
}

func cloneBootstrapTelemetry(telemetry *bootstrapTelemetry) *bootstrapTelemetry {
	if telemetry == nil {
		return nil
	}
	clone := &bootstrapTelemetry{
		Enabled: cloneBoolPointer(telemetry.Enabled),
	}
	if telemetry.Exporter != nil {
		clone.Exporter = &bootstrapTelemetryExporter{
			Endpoint:    cloneStringPointer(telemetry.Exporter.Endpoint),
			Protocol:    cloneStringPointer(telemetry.Exporter.Protocol),
			Compression: cloneStringPointer(telemetry.Exporter.Compression),
			Timeout:     cloneStringPointer(telemetry.Exporter.Timeout),
		}
		if telemetry.Exporter.Auth != nil {
			clone.Exporter.Auth = &bootstrapTelemetryExporterAuth{
				Mode:                cloneStringPointer(telemetry.Exporter.Auth.Mode),
				AuthorizationHeader: cloneStringPointer(telemetry.Exporter.Auth.AuthorizationHeader),
			}
		}
		if telemetry.Exporter.TLS != nil {
			clone.Exporter.TLS = &bootstrapTelemetryExporterTLS{
				InsecureSkipVerify: cloneBoolPointer(telemetry.Exporter.TLS.InsecureSkipVerify),
				CAFile:             cloneStringPointer(telemetry.Exporter.TLS.CAFile),
			}
		}
	}
	if telemetry.Metrics != nil {
		clone.Metrics = &bootstrapTelemetrySignal{Enabled: cloneBoolPointer(telemetry.Metrics.Enabled)}
	}
	if telemetry.Traces != nil {
		clone.Traces = &bootstrapTelemetryTraces{Enabled: cloneBoolPointer(telemetry.Traces.Enabled), SamplingRatio: cloneFloat64Pointer(telemetry.Traces.SamplingRatio)}
	}
	return clone
}

func safeBootstrapServerValues(server *bootstrapServer) *BootstrapConfigServerValues {
	if server == nil {
		return nil
	}
	return &BootstrapConfigServerValues{Host: cloneStringPointer(server.Host), Port: cloneIntPointer(server.Port)}
}

func safeBootstrapDatabaseValues(database *bootstrapDatabase) *BootstrapConfigDatabaseValues {
	if database == nil {
		return nil
	}
	return &BootstrapConfigDatabaseValues{
		Pools:               safeBootstrapDatabasePoolsValues(database.Pools),
		ManagementAdmission: safeBootstrapManagementAdmissionValues(database.ManagementAdmission),
	}
}

func safeBootstrapDatabasePoolsValues(pools *bootstrapDatabasePools) *BootstrapConfigDatabasePoolsValues {
	if pools == nil {
		return nil
	}
	return &BootstrapConfigDatabasePoolsValues{
		TotalMaxConns:    cloneIntPointer(pools.TotalMaxConns),
		Management:       safeBootstrapDatabasePoolValues(pools.Management),
		RuntimeExecution: safeBootstrapDatabasePoolValues(pools.RuntimeExecution),
		RuntimeTelemetry: safeBootstrapDatabasePoolValues(pools.RuntimeTelemetry),
		RuntimeFeedback:  safeBootstrapDatabasePoolValues(pools.RuntimeFeedback),
		Realtime:         safeBootstrapDatabasePoolValues(pools.Realtime),
		CacheRefresh:     safeBootstrapDatabasePoolValues(pools.CacheRefresh),
		BackgroundJobs:   safeBootstrapDatabasePoolValues(pools.BackgroundJobs),
	}
}

func safeBootstrapDatabasePoolValues(pool *bootstrapDatabasePool) *BootstrapConfigDatabasePoolValues {
	if pool == nil {
		return nil
	}
	return &BootstrapConfigDatabasePoolValues{MaxConns: cloneIntPointer(pool.MaxConns), MinIdleConns: cloneIntPointer(pool.MinIdleConns)}
}

func safeBootstrapManagementAdmissionValues(admission *bootstrapManagementAdmission) *BootstrapConfigManagementAdmissionValues {
	if admission == nil {
		return nil
	}
	return &BootstrapConfigManagementAdmissionValues{M2MaxConcurrent: cloneIntPointer(admission.M2MaxConcurrent), M3MaxConcurrent: cloneIntPointer(admission.M3MaxConcurrent)}
}

func safeBootstrapRuntimeValues(runtimeConfig *bootstrapRuntime) *BootstrapConfigRuntimeValues {
	if runtimeConfig == nil {
		return nil
	}
	return &BootstrapConfigRuntimeValues{
		Transport:   safeBootstrapRuntimeTransportValues(runtimeConfig.Transport),
		SideEffects: safeBootstrapRuntimeSideEffectsValues(runtimeConfig.SideEffects),
		Routing:     safeBootstrapRuntimeRoutingValues(runtimeConfig.Routing),
	}
}

func safeBootstrapRuntimeTransportValues(transport *bootstrapRuntimeTransport) *BootstrapConfigRuntimeTransportValues {
	if transport == nil {
		return nil
	}
	return &BootstrapConfigRuntimeTransportValues{
		MaxIdleConns:          cloneIntPointer(transport.MaxIdleConns),
		MaxIdleConnsPerHost:   cloneIntPointer(transport.MaxIdleConnsPerHost),
		MaxConnsPerHost:       cloneIntPointer(transport.MaxConnsPerHost),
		RequestTimeout:        cloneStringPointer(transport.RequestTimeout),
		IdleConnTimeout:       cloneStringPointer(transport.IdleConnTimeout),
		ResponseHeaderTimeout: cloneStringPointer(transport.ResponseHeaderTimeout),
		TLSHandshakeTimeout:   cloneStringPointer(transport.TLSHandshakeTimeout),
		ExpectContinueTimeout: cloneStringPointer(transport.ExpectContinueTimeout),
	}
}

func safeBootstrapRuntimeSideEffectsValues(sideEffects *bootstrapRuntimeSideEffects) *BootstrapConfigRuntimeSideEffectsValues {
	if sideEffects == nil {
		return nil
	}
	return &BootstrapConfigRuntimeSideEffectsValues{AttemptTimeout: cloneStringPointer(sideEffects.AttemptTimeout)}
}

func safeBootstrapRuntimeRoutingValues(*bootstrapRuntimeRouting) *BootstrapConfigRuntimeRoutingValues {
	return defaultSafeBootstrapRuntimeRoutingValues()
}

func safeBootstrapHTTPValues(httpConfig *bootstrapHTTP) *BootstrapConfigHTTPValues {
	if httpConfig == nil {
		return nil
	}
	return &BootstrapConfigHTTPValues{CORSAllowedOrigins: cloneStringSlicePointer(httpConfig.CORSAllowedOrigins)}
}

func safeBootstrapAuthValues(auth *bootstrapAuth) *BootstrapConfigAuthValues {
	if auth == nil {
		return nil
	}
	return &BootstrapConfigAuthValues{
		AccessTokenTTLSeconds:  cloneIntPointer(auth.AccessTokenTTLSeconds),
		RefreshTokenTTLSeconds: cloneIntPointer(auth.RefreshTokenTTLSeconds),
		ResetCodeTTLSeconds:    cloneIntPointer(auth.ResetCodeTTLSeconds),
		AccessCookieName:       cloneStringPointer(auth.AccessCookieName),
		RefreshCookieName:      cloneStringPointer(auth.RefreshCookieName),
		CookieSecure:           cloneBoolPointer(auth.CookieSecure),
	}
}

func safeBootstrapMailValues(mailConfig *bootstrapMail) *BootstrapConfigMailValues {
	if mailConfig == nil {
		return canonicalDisabledBootstrapMailValues()
	}
	return &BootstrapConfigMailValues{
		Enabled: cloneBoolPointer(mailConfig.Enabled),
		From:    cloneStringPointer(mailConfig.From),
		ReplyTo: cloneStringPointer(mailConfig.ReplyTo),
		SMTP:    safeBootstrapSMTPValues(mailConfig.SMTP),
	}
}

func canonicalDisabledBootstrapMailValues() *BootstrapConfigMailValues {
	return &BootstrapConfigMailValues{Enabled: boolPointer(false)}
}

func canonicalDisabledBootstrapMailDocument() *bootstrapMail {
	return &bootstrapMail{Enabled: boolPointer(false)}
}

func safeBootstrapSMTPValues(smtp *bootstrapSMTP) *BootstrapConfigMailSMTPValues {
	if smtp == nil {
		return nil
	}
	return &BootstrapConfigMailSMTPValues{
		Host:          cloneStringPointer(smtp.Host),
		Port:          cloneIntPointer(smtp.Port),
		Mode:          cloneStringPointer(smtp.Mode),
		EHLOHostname:  cloneStringPointer(smtp.EHLOHostname),
		Auth:          cloneStringPointer(smtp.Auth),
		Username:      cloneStringPointer(smtp.Username),
		PasswordFile:  cloneStringPointer(smtp.PasswordFile),
		Timeout:       cloneStringPointer(smtp.Timeout),
		TLSServerName: cloneStringPointer(smtp.TLSServerName),
	}
}

func safeBootstrapTelemetryValues(telemetry *bootstrapTelemetry) *BootstrapConfigTelemetryValues {
	if telemetry == nil {
		return canonicalDisabledBootstrapTelemetryValues()
	}
	return &BootstrapConfigTelemetryValues{
		Enabled:  cloneBoolPointer(telemetry.Enabled),
		Exporter: safeBootstrapTelemetryExporterValues(telemetry.Exporter),
		Metrics:  safeBootstrapTelemetrySignalValues(telemetry.Metrics),
		Traces:   safeBootstrapTelemetryTracesValues(telemetry.Traces),
	}
}

func canonicalDisabledBootstrapTelemetryValues() *BootstrapConfigTelemetryValues {
	return &BootstrapConfigTelemetryValues{Enabled: boolPointer(false)}
}

func canonicalDisabledBootstrapTelemetryDocument() *bootstrapTelemetry {
	return &bootstrapTelemetry{Enabled: boolPointer(false)}
}

func safeBootstrapTelemetryExporterValues(exporter *bootstrapTelemetryExporter) *BootstrapConfigTelemetryExporterValues {
	if exporter == nil {
		return nil
	}
	return &BootstrapConfigTelemetryExporterValues{
		Endpoint:    cloneStringPointer(exporter.Endpoint),
		Protocol:    cloneStringPointer(exporter.Protocol),
		Compression: cloneStringPointer(exporter.Compression),
		Timeout:     cloneStringPointer(exporter.Timeout),
		Auth:        safeBootstrapTelemetryExporterAuthValues(exporter.Auth),
		TLS:         safeBootstrapTelemetryExporterTLSValues(exporter.TLS),
	}
}

func safeBootstrapTelemetryExporterAuthValues(auth *bootstrapTelemetryExporterAuth) *BootstrapConfigTelemetryExporterAuthValues {
	if auth == nil {
		return nil
	}
	return &BootstrapConfigTelemetryExporterAuthValues{Mode: cloneStringPointer(auth.Mode)}
}

func safeBootstrapTelemetryExporterTLSValues(tlsConfig *bootstrapTelemetryExporterTLS) *BootstrapConfigTelemetryExporterTLSValues {
	if tlsConfig == nil {
		return nil
	}
	return &BootstrapConfigTelemetryExporterTLSValues{
		InsecureSkipVerify: cloneBoolPointer(tlsConfig.InsecureSkipVerify),
		CAFile:             cloneStringPointer(tlsConfig.CAFile),
	}
}

func safeBootstrapTelemetrySignalValues(signal *bootstrapTelemetrySignal) *BootstrapConfigTelemetrySignalValues {
	if signal == nil {
		return nil
	}
	return &BootstrapConfigTelemetrySignalValues{Enabled: cloneBoolPointer(signal.Enabled)}
}

func safeBootstrapTelemetryTracesValues(traces *bootstrapTelemetryTraces) *BootstrapConfigTelemetryTracesValues {
	if traces == nil {
		return nil
	}
	return &BootstrapConfigTelemetryTracesValues{Enabled: cloneBoolPointer(traces.Enabled), SamplingRatio: cloneFloat64Pointer(traces.SamplingRatio)}
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	return intPointer(*value)
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPointer(*value)
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	return boolPointer(*value)
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	return float64Pointer(*value)
}

func cloneStringSlicePointer(value *[]string) *[]string {
	if value == nil {
		return nil
	}
	clone := append([]string(nil), (*value)...)
	return &clone
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}

func resolveBootstrapExternalInputsFromEnv() (string, error) {
	configPath := strings.TrimSpace(os.Getenv(BootstrapConfigPathEnv))
	if configPath == "" {
		return defaultBootstrapConfigPath, nil
	}
	return configPath, nil
}

func (m BootstrapConfigManager) seedPayloadFromDefaults() ([]byte, Settings, error) {
	document, err := buildSeededBootstrapDocument(Load(), m.resolvedTimeNow()().UTC())
	if err != nil {
		return nil, Settings{}, err
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, Settings{}, fmt.Errorf("marshal seeded bootstrap config JSON: %w", err)
	}
	settings, err := m.Parse(raw)
	if err != nil {
		return nil, Settings{}, fmt.Errorf("validate seeded bootstrap config: %w", err)
	}
	return raw, settings, nil
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

func decodeBootstrapConfig(raw []byte) (bootstrapConfigDocument, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document bootstrapConfigDocument
	if err := decoder.Decode(&document); err != nil {
		return bootstrapConfigDocument{}, fmt.Errorf("decode bootstrap config JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return bootstrapConfigDocument{}, fmt.Errorf("bootstrap config JSON must contain a single object")
		}
		return bootstrapConfigDocument{}, fmt.Errorf("decode bootstrap config JSON: %w", err)
	}
	return document, nil
}

func (d bootstrapConfigDocument) validateSchema() error {
	if d.Meta == nil {
		return missingBootstrapFieldError("meta")
	}
	if d.Server == nil {
		return missingBootstrapFieldError("server")
	}
	if d.Database == nil {
		return missingBootstrapFieldError("database")
	}
	if d.Runtime == nil {
		return missingBootstrapFieldError("runtime")
	}
	if d.HTTP == nil {
		return missingBootstrapFieldError("http")
	}
	if d.Auth == nil {
		return missingBootstrapFieldError("auth")
	}
	if d.StateTransfer == nil {
		return missingBootstrapFieldError("stateTransfer")
	}
	if err := d.Meta.validate(); err != nil {
		return err
	}
	if err := d.Server.validate(); err != nil {
		return err
	}
	if err := d.Database.validate(); err != nil {
		return err
	}
	if err := d.Runtime.validate(); err != nil {
		return err
	}
	if err := d.HTTP.validate(); err != nil {
		return err
	}
	if err := d.Auth.validate(); err != nil {
		return err
	}
	if d.Mail != nil {
		if err := d.Mail.validate(); err != nil {
			return err
		}
	}
	if d.Telemetry != nil {
		if err := d.Telemetry.validate(); err != nil {
			return err
		}
	}
	return d.StateTransfer.validate()
}

func (m bootstrapMeta) validate() error {
	if _, err := requiredIntConst("meta.schemaVersion", m.SchemaVersion, bootstrapConfigSchemaVersion); err != nil {
		return err
	}
	if _, err := requiredIntMin("meta.revision", m.Revision, 1); err != nil {
		return err
	}
	if _, err := requiredDateTime("meta.createdAt", m.CreatedAt); err != nil {
		return err
	}
	if _, err := requiredDateTime("meta.updatedAt", m.UpdatedAt); err != nil {
		return err
	}
	return nil
}

func (s bootstrapServer) validate() error {
	if _, err := requiredTrimmedString("server.host", s.Host, 1, 255); err != nil {
		return err
	}
	if _, err := requiredIntRange("server.port", s.Port, 1, 65535); err != nil {
		return err
	}
	return nil
}

func (d bootstrapDatabase) validate() error {
	if _, err := requiredTrimmedString("database.url", d.URL, 1, 0); err != nil {
		return err
	}
	if d.Pools == nil {
		return fmt.Errorf("invalid postgres pool config: pools are required")
	}
	if d.ManagementAdmission == nil {
		return missingBootstrapFieldError("database.managementAdmission")
	}
	if _, err := d.Pools.toPostgresPoolsBudget(); err != nil {
		return err
	}
	return d.ManagementAdmission.validate()
}

func (p bootstrapDatabasePool) toDatabasePoolBudget(path string) (DatabasePoolBudget, error) {
	maxConns, err := requiredIntRange(path+".maxConns", p.MaxConns, 1, math.MaxInt32)
	if err != nil {
		return DatabasePoolBudget{}, err
	}
	minIdleConns, err := requiredIntRange(path+".minIdleConns", p.MinIdleConns, 0, math.MaxInt32)
	if err != nil {
		return DatabasePoolBudget{}, err
	}
	return DatabasePoolBudget{MaxConns: int32(maxConns), MinIdleConns: int32(minIdleConns)}, nil
}

func (p *bootstrapDatabasePools) toPostgresPoolsBudget() (PostgresPoolsBudget, error) {
	if p == nil {
		return PostgresPoolsBudget{}, fmt.Errorf("invalid postgres pool config: pools are required")
	}
	totalMaxConns, err := requiredIntRange("database.pools.totalMaxConns", p.TotalMaxConns, 1, math.MaxInt32)
	if err != nil {
		return PostgresPoolsBudget{}, fmt.Errorf("invalid postgres pool config: total_max_conns must be greater than zero")
	}
	lanePool := func(lane PostgresPoolLane, pool *bootstrapDatabasePool) (DatabasePoolBudget, error) {
		if pool == nil {
			return DatabasePoolBudget{}, fmt.Errorf("invalid postgres pool config: lane=%s is required", lane)
		}
		budget, err := pool.toDatabasePoolBudget("database.pools." + bootstrapDatabasePoolPath(lane))
		if err != nil {
			if strings.Contains(err.Error(), ".maxConns") {
				return DatabasePoolBudget{}, fmt.Errorf("invalid postgres pool config: lane=%s max_conns must be greater than zero", lane)
			}
			if strings.Contains(err.Error(), ".minIdleConns") {
				return DatabasePoolBudget{}, fmt.Errorf("invalid postgres pool config: lane=%s min_idle_conns must be greater than or equal to zero", lane)
			}
			return DatabasePoolBudget{}, err
		}
		return budget, nil
	}
	budget := PostgresPoolsBudget{TotalMaxConns: int32(totalMaxConns)}
	if budget.Management, err = lanePool(PostgresLaneManagement, p.Management); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.RuntimeExecution, err = lanePool(PostgresLaneRuntimeExecution, p.RuntimeExecution); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.RuntimeTelemetry, err = lanePool(PostgresLaneRuntimeTelemetry, p.RuntimeTelemetry); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.RuntimeFeedback, err = lanePool(PostgresLaneRuntimeFeedback, p.RuntimeFeedback); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.Realtime, err = lanePool(PostgresLaneRealtime, p.Realtime); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.CacheRefresh, err = lanePool(PostgresLaneCacheRefresh, p.CacheRefresh); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if budget.BackgroundJobs, err = lanePool(PostgresLaneBackgroundJobs, p.BackgroundJobs); err != nil {
		return PostgresPoolsBudget{}, err
	}
	if err := budget.Validate(); err != nil {
		return PostgresPoolsBudget{}, err
	}
	return budget, nil
}

func bootstrapDatabasePoolPath(lane PostgresPoolLane) string {
	switch lane {
	case PostgresLaneRuntimeExecution:
		return "runtimeExecution"
	case PostgresLaneRuntimeTelemetry:
		return "runtimeTelemetry"
	case PostgresLaneRuntimeFeedback:
		return "runtimeFeedback"
	case PostgresLaneCacheRefresh:
		return "cacheRefresh"
	case PostgresLaneBackgroundJobs:
		return "backgroundJobs"
	default:
		return string(lane)
	}
}

func (a bootstrapManagementAdmission) validate() error {
	if _, err := requiredIntMin("database.managementAdmission.m2MaxConcurrent", a.M2MaxConcurrent, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("database.managementAdmission.m3MaxConcurrent", a.M3MaxConcurrent, 1); err != nil {
		return err
	}
	return nil
}

func (r bootstrapRuntime) validate() error {
	if _, err := requiredTrimmedString("secretEncryptionKey", r.SecretEncryptionKey, 1, 0); err != nil {
		return err
	}
	if r.Transport == nil {
		return missingBootstrapFieldError("transport")
	}
	if r.SideEffects == nil {
		return missingBootstrapFieldError("sideEffects")
	}
	if err := r.Transport.validate(); err != nil {
		return err
	}
	if err := r.SideEffects.validate(); err != nil {
		return err
	}
	if r.Routing == nil {
		return nil
	}
	return r.Routing.validate()
}

func (t bootstrapRuntimeTransport) validate() error {
	if _, err := requiredIntMin("transport.maxIdleConns", t.MaxIdleConns, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("transport.maxIdleConnsPerHost", t.MaxIdleConnsPerHost, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("transport.maxConnsPerHost", t.MaxConnsPerHost, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("transport.requestTimeout", t.RequestTimeout, 1, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("transport.idleConnTimeout", t.IdleConnTimeout, 1, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("transport.responseHeaderTimeout", t.ResponseHeaderTimeout, 1, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("transport.tlsHandshakeTimeout", t.TLSHandshakeTimeout, 1, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("transport.expectContinueTimeout", t.ExpectContinueTimeout, 1, 0); err != nil {
		return err
	}
	return nil
}

func (s bootstrapRuntimeSideEffects) validate() error {
	_, err := requiredTrimmedString("sideEffects.attemptTimeout", s.AttemptTimeout, 1, 0)
	return err
}

func (r bootstrapRuntimeRouting) validate() error {
	return nil
}

func (h bootstrapHTTP) validate() error {
	_, err := requiredAbsoluteURIs("http.corsAllowedOrigins", h.CORSAllowedOrigins)
	return err
}

func (a bootstrapAuth) validate() error {
	if _, err := requiredTrimmedString("auth.jwtSigningKey", a.JWTSigningKey, 1, 0); err != nil {
		return err
	}
	if _, err := requiredIntMin("auth.accessTokenTtlSeconds", a.AccessTokenTTLSeconds, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("auth.refreshTokenTtlSeconds", a.RefreshTokenTTLSeconds, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("auth.resetCodeTtlSeconds", a.ResetCodeTTLSeconds, 1); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("auth.accessCookieName", a.AccessCookieName, 1, 200); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("auth.refreshCookieName", a.RefreshCookieName, 1, 200); err != nil {
		return err
	}
	_, err := requiredBool("auth.cookieSecure", a.CookieSecure)
	return err
}

func (m bootstrapMail) validate() error {
	enabled, err := requiredBool("mail.enabled", m.Enabled)
	if err != nil {
		return err
	}
	if m.From != nil {
		if _, err := optionalMailAddress("mail.from", m.From); err != nil {
			return err
		}
	}
	if m.ReplyTo != nil {
		if _, err := optionalMailAddress("mail.replyTo", m.ReplyTo); err != nil {
			return err
		}
	}
	if enabled {
		if _, err := requiredMailAddress("mail.from", m.From); err != nil {
			return err
		}
		if m.SMTP == nil {
			return missingBootstrapFieldError("mail.smtp")
		}
	}
	if m.SMTP != nil {
		return m.SMTP.validate(enabled)
	}
	return nil
}

func (s bootstrapSMTP) validate(enabled bool) error {
	if enabled {
		if _, err := requiredTrimmedString("mail.smtp.host", s.Host, 1, 255); err != nil {
			return err
		}
		if _, err := requiredIntRange("mail.smtp.port", s.Port, 1, 65535); err != nil {
			return err
		}
		if _, err := requiredEnumString("mail.smtp.mode", s.Mode, allowedMailSMTPModes()); err != nil {
			return err
		}
		if _, err := parseDurationField("mail.smtp.timeout", s.Timeout); err != nil {
			return err
		}
	}
	if s.Host != nil {
		if _, err := optionalTrimmedString("mail.smtp.host", s.Host, 255); err != nil {
			return err
		}
	}
	if s.Port != nil {
		if _, err := requiredIntRange("mail.smtp.port", s.Port, 1, 65535); err != nil {
			return err
		}
	}
	if s.Mode != nil {
		if _, err := requiredEnumString("mail.smtp.mode", s.Mode, allowedMailSMTPModes()); err != nil {
			return err
		}
	}
	if s.Auth != nil {
		if _, err := requiredEnumString("mail.smtp.auth", s.Auth, allowedMailSMTPAuthModes()); err != nil {
			return err
		}
	}
	if s.EHLOHostname != nil {
		if _, err := optionalTrimmedString("mail.smtp.ehloHostname", s.EHLOHostname, 255); err != nil {
			return err
		}
	}
	if s.Username != nil {
		if _, err := optionalTrimmedString("mail.smtp.username", s.Username, 320); err != nil {
			return err
		}
	}
	if s.PasswordFile != nil {
		if _, err := optionalTrimmedString("mail.smtp.passwordFile", s.PasswordFile, 0); err != nil {
			return err
		}
	}
	if s.TLSServerName != nil {
		if _, err := optionalTrimmedString("mail.smtp.tlsServerName", s.TLSServerName, 255); err != nil {
			return err
		}
	}
	if s.Timeout != nil {
		if _, err := parseDurationField("mail.smtp.timeout", s.Timeout); err != nil {
			return err
		}
	}
	if hasNonEmptyString(s.Password) && hasNonEmptyString(s.PasswordFile) {
		return fmt.Errorf("bootstrap config fields mail.smtp.password and mail.smtp.passwordFile are mutually exclusive")
	}
	if enabled {
		auth := normalizedMailSMTPAuth(s.Auth)
		if auth == MailSMTPAuthPlain {
			if _, err := requiredTrimmedString("mail.smtp.username", s.Username, 1, 320); err != nil {
				return err
			}
			if !hasNonEmptyString(s.Password) && !hasNonEmptyString(s.PasswordFile) {
				return fmt.Errorf("bootstrap config field mail.smtp.password or mail.smtp.passwordFile is required when mail.smtp.auth is plain")
			}
		}
		mode := normalizedMailSMTPMode(s.Mode)
		if mode == MailSMTPModePlaintextLocalOnly {
			if !isLocalSMTPHost(strings.TrimSpace(*s.Host)) {
				return fmt.Errorf("bootstrap config field mail.smtp.mode plaintext_local_only requires a localhost or loopback host")
			}
		}
	}
	return nil
}

func (s bootstrapStateTransfer) validate() error {
	_, err := requiredTrimmedString("stateTransfer.bundleEncryptionKey", s.BundleEncryptionKey, 1, 0)
	return err
}

func (d bootstrapConfigDocument) validateSemantics() error {
	for _, field := range []struct {
		path  string
		value *string
	}{
		{path: "transport.requestTimeout", value: d.Runtime.Transport.RequestTimeout},
		{path: "transport.idleConnTimeout", value: d.Runtime.Transport.IdleConnTimeout},
		{path: "transport.responseHeaderTimeout", value: d.Runtime.Transport.ResponseHeaderTimeout},
		{path: "transport.tlsHandshakeTimeout", value: d.Runtime.Transport.TLSHandshakeTimeout},
		{path: "transport.expectContinueTimeout", value: d.Runtime.Transport.ExpectContinueTimeout},
		{path: "sideEffects.attemptTimeout", value: d.Runtime.SideEffects.AttemptTimeout},
	} {
		if _, err := parseDurationField(field.path, field.value); err != nil {
			return err
		}
	}
	if _, err := d.Database.Pools.toPostgresPoolsBudget(); err != nil {
		return err
	}
	m2MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m2MaxConcurrent", d.Database.ManagementAdmission.M2MaxConcurrent, 1)
	m3MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m3MaxConcurrent", d.Database.ManagementAdmission.M3MaxConcurrent, 1)
	if m3MaxConcurrent > m2MaxConcurrent {
		return fmt.Errorf("bootstrap config field database.managementAdmission.m3MaxConcurrent must be less than or equal to database.managementAdmission.m2MaxConcurrent")
	}
	if d.Mail != nil {
		return d.Mail.validate()
	}
	return nil
}

func (d bootstrapConfigDocument) toSettings() (Settings, error) {
	host, err := requiredTrimmedString("server.host", d.Server.Host, 1, 255)
	if err != nil {
		return Settings{}, err
	}
	port, err := requiredIntRange("server.port", d.Server.Port, 1, 65535)
	if err != nil {
		return Settings{}, err
	}
	databaseURL, err := requiredTrimmedString("database.url", d.Database.URL, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	runtimeSecretEncryptionKey, err := requiredTrimmedString("secretEncryptionKey", d.Runtime.SecretEncryptionKey, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	runtimeTransport, err := d.Runtime.Transport.toRuntimeTransportConfig()
	if err != nil {
		return Settings{}, err
	}
	runtimeSideEffects, err := d.Runtime.SideEffects.toRuntimeSideEffectsConfig()
	if err != nil {
		return Settings{}, err
	}
	postgresPoolsBudget, err := d.Database.Pools.toPostgresPoolsBudget()
	if err != nil {
		return Settings{}, err
	}
	m2MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m2MaxConcurrent", d.Database.ManagementAdmission.M2MaxConcurrent, 1)
	m3MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m3MaxConcurrent", d.Database.ManagementAdmission.M3MaxConcurrent, 1)
	corsAllowedOrigins, err := requiredAbsoluteURIs("http.corsAllowedOrigins", d.HTTP.CORSAllowedOrigins)
	if err != nil {
		return Settings{}, err
	}
	jwtSigningKey, err := requiredTrimmedString("auth.jwtSigningKey", d.Auth.JWTSigningKey, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	bundleEncryptionKey, err := requiredTrimmedString("stateTransfer.bundleEncryptionKey", d.StateTransfer.BundleEncryptionKey, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	accessTokenTTLSeconds, _ := requiredIntMin("auth.accessTokenTtlSeconds", d.Auth.AccessTokenTTLSeconds, 1)
	refreshTokenTTLSeconds, _ := requiredIntMin("auth.refreshTokenTtlSeconds", d.Auth.RefreshTokenTTLSeconds, 1)
	resetCodeTTLSeconds, _ := requiredIntMin("auth.resetCodeTtlSeconds", d.Auth.ResetCodeTTLSeconds, 1)
	accessCookieName, _ := requiredTrimmedString("auth.accessCookieName", d.Auth.AccessCookieName, 1, 200)
	refreshCookieName, _ := requiredTrimmedString("auth.refreshCookieName", d.Auth.RefreshCookieName, 1, 200)
	cookieSecure, _ := requiredBool("auth.cookieSecure", d.Auth.CookieSecure)
	mailConfig, err := d.Mail.toMailConfig()
	if err != nil {
		return Settings{}, err
	}
	telemetryConfig, err := d.Telemetry.toTelemetryConfig()
	if err != nil {
		return Settings{}, err
	}

	return Settings{
		Host:                             host,
		Port:                             port,
		AppEnv:                           EnvironmentDevelopment,
		DatabaseURL:                      databaseURL,
		RuntimeTelemetryMode:             RuntimeTelemetryModeDurableOutbox,
		Telemetry:                        telemetryConfig,
		RuntimeTransportConfig:           runtimeTransport,
		RuntimeSideEffectsConfig:         runtimeSideEffects,
		PostgresPoolsBudget:              postgresPoolsBudget,
		RuntimeDatabasePoolBudget:        postgresPoolsBudget.RuntimeExecution,
		ManagementDatabasePoolBudget:     postgresPoolsBudget.Management,
		ManagementAdmissionControlBudget: ManagementAdmissionBudget{M2MaxConcurrent: int64(m2MaxConcurrent), M3MaxConcurrent: int64(m3MaxConcurrent)},
		SecretEncryptionKey:              runtimeSecretEncryptionKey,
		ConfigBundleEncryptionKey:        bundleEncryptionKey,
		CORSAllowedOrigins:               strings.Join(corsAllowedOrigins, ","),
		AuthJWTSecret:                    jwtSigningKey,
		AuthAccessTokenTTLSeconds:        accessTokenTTLSeconds,
		AuthRefreshTokenTTLSeconds:       refreshTokenTTLSeconds,
		AuthResetCodeTTLSeconds:          resetCodeTTLSeconds,
		AuthCookieName:                   accessCookieName,
		AuthRefreshCookieName:            refreshCookieName,
		AuthCookieSecure:                 cookieSecure,
		Mail:                             mailConfig,
	}, nil
}

func (m *bootstrapMail) toMailConfig() (MailConfig, error) {
	if m == nil {
		return defaultMailConfig(), nil
	}
	enabled, err := requiredBool("mail.enabled", m.Enabled)
	if err != nil {
		return MailConfig{}, err
	}
	result := defaultMailConfig()
	result.Enabled = enabled
	if m.From != nil {
		from, err := optionalMailAddress("mail.from", m.From)
		if err != nil {
			return MailConfig{}, err
		}
		result.From = from
	}
	if enabled {
		from, err := requiredMailAddress("mail.from", m.From)
		if err != nil {
			return MailConfig{}, err
		}
		result.From = from
	}
	if m.ReplyTo != nil {
		replyTo, err := optionalMailAddress("mail.replyTo", m.ReplyTo)
		if err != nil {
			return MailConfig{}, err
		}
		result.ReplyTo = replyTo
	}
	if m.SMTP == nil {
		return result, nil
	}
	smtp, err := m.SMTP.toMailSMTPConfig(enabled)
	if err != nil {
		return MailConfig{}, err
	}
	result.SMTP = smtp
	return result, nil
}

func (s bootstrapSMTP) toMailSMTPConfig(enabled bool) (MailSMTPConfig, error) {
	result := MailSMTPConfig{Timeout: defaultMailSMTPTimeout}
	if s.Host != nil {
		result.Host = strings.TrimSpace(*s.Host)
	}
	if s.Port != nil {
		port, err := requiredIntRange("mail.smtp.port", s.Port, 1, 65535)
		if err != nil {
			return MailSMTPConfig{}, err
		}
		result.Port = port
	}
	if s.Mode != nil {
		mode, err := requiredEnumString("mail.smtp.mode", s.Mode, allowedMailSMTPModes())
		if err != nil {
			return MailSMTPConfig{}, err
		}
		result.Mode = MailSMTPMode(mode)
	}
	if s.EHLOHostname != nil {
		result.EHLOHostname = strings.TrimSpace(*s.EHLOHostname)
	}
	if s.Auth != nil {
		auth, err := requiredEnumString("mail.smtp.auth", s.Auth, allowedMailSMTPAuthModes())
		if err != nil {
			return MailSMTPConfig{}, err
		}
		result.Auth = MailSMTPAuth(auth)
	}
	if s.Username != nil {
		result.Username = strings.TrimSpace(*s.Username)
	}
	if s.Password != nil {
		result.Password = strings.TrimSpace(*s.Password)
	}
	if s.PasswordFile != nil {
		result.PasswordFile = strings.TrimSpace(*s.PasswordFile)
	}
	if s.Timeout != nil {
		timeout, err := parseDurationField("mail.smtp.timeout", s.Timeout)
		if err != nil {
			return MailSMTPConfig{}, err
		}
		result.Timeout = timeout
	}
	if s.TLSServerName != nil {
		result.TLSServerName = strings.TrimSpace(*s.TLSServerName)
	}
	if enabled && result.Auth == "" {
		result.Auth = MailSMTPAuthNone
	}
	return result, nil
}

func (t *bootstrapTelemetry) validate() error {
	_, err := t.toTelemetryConfig()
	return err
}

func (t *bootstrapTelemetry) toTelemetryConfig() (TelemetryConfig, error) {
	result := defaultTelemetryConfig()
	if t == nil {
		return result, nil
	}
	enabled, err := requiredBool("telemetry.enabled", t.Enabled)
	if err != nil {
		return TelemetryConfig{}, err
	}
	result.Enabled = enabled
	exporter, err := t.Exporter.toTelemetryExporterConfig(enabled)
	if err != nil {
		return TelemetryConfig{}, err
	}
	result.Exporter = exporter
	metrics, err := t.Metrics.toTelemetrySignalConfig("telemetry.metrics", enabled)
	if err != nil {
		return TelemetryConfig{}, err
	}
	result.Metrics = metrics
	traces, err := t.Traces.toTelemetryTracesConfig(enabled)
	if err != nil {
		return TelemetryConfig{}, err
	}
	result.Traces = traces
	return result, nil
}

func (e *bootstrapTelemetryExporter) toTelemetryExporterConfig(enabled bool) (TelemetryExporterConfig, error) {
	result := defaultTelemetryConfig().Exporter
	if e == nil {
		if enabled {
			return TelemetryExporterConfig{}, missingBootstrapFieldError("telemetry.exporter")
		}
		return result, nil
	}
	if e.Endpoint != nil {
		endpoint, err := requiredTrimmedString("telemetry.exporter.endpoint", e.Endpoint, 1, 2048)
		if err != nil {
			return TelemetryExporterConfig{}, err
		}
		result.Endpoint = endpoint
	} else if enabled {
		return TelemetryExporterConfig{}, missingBootstrapFieldError("telemetry.exporter.endpoint")
	}
	if e.Protocol != nil {
		protocol, err := requiredEnumString("telemetry.exporter.protocol", e.Protocol, allowedTelemetryExporterProtocols())
		if err != nil {
			return TelemetryExporterConfig{}, err
		}
		result.Protocol = TelemetryExporterProtocol(protocol)
	} else if enabled {
		return TelemetryExporterConfig{}, missingBootstrapFieldError("telemetry.exporter.protocol")
	}
	if e.Compression != nil {
		compression, err := requiredEnumString("telemetry.exporter.compression", e.Compression, allowedTelemetryExporterCompressions())
		if err != nil {
			return TelemetryExporterConfig{}, err
		}
		result.Compression = TelemetryExporterCompression(compression)
	} else if enabled {
		return TelemetryExporterConfig{}, missingBootstrapFieldError("telemetry.exporter.compression")
	}
	if e.Timeout != nil {
		timeout, err := parseDurationField("telemetry.exporter.timeout", e.Timeout)
		if err != nil {
			return TelemetryExporterConfig{}, err
		}
		if timeout <= 0 {
			return TelemetryExporterConfig{}, fmt.Errorf("bootstrap config field telemetry.exporter.timeout must be greater than zero")
		}
		result.Timeout = timeout
	} else if enabled {
		return TelemetryExporterConfig{}, missingBootstrapFieldError("telemetry.exporter.timeout")
	}
	auth, err := e.Auth.toTelemetryExporterAuthConfig(enabled)
	if err != nil {
		return TelemetryExporterConfig{}, err
	}
	result.Auth = auth
	tlsConfig, err := e.TLS.toTelemetryExporterTLSConfig(enabled)
	if err != nil {
		return TelemetryExporterConfig{}, err
	}
	result.TLS = tlsConfig
	return result, nil
}

func (a *bootstrapTelemetryExporterAuth) toTelemetryExporterAuthConfig(enabled bool) (TelemetryExporterAuthConfig, error) {
	result := TelemetryExporterAuthConfig{Mode: defaultTelemetryExporterAuthMode}
	if a == nil {
		if enabled {
			return TelemetryExporterAuthConfig{}, missingBootstrapFieldError("telemetry.exporter.auth")
		}
		return result, nil
	}
	if a.Mode != nil {
		mode, err := requiredEnumString("telemetry.exporter.auth.mode", a.Mode, allowedTelemetryExporterAuthModes())
		if err != nil {
			return TelemetryExporterAuthConfig{}, err
		}
		result.Mode = TelemetryExporterAuthMode(mode)
	} else if enabled {
		return TelemetryExporterAuthConfig{}, missingBootstrapFieldError("telemetry.exporter.auth.mode")
	}
	if result.Mode == TelemetryExporterAuthModeAuthorizationHeader {
		header, err := requiredTrimmedString("telemetry.exporter.auth.authorizationHeader", a.AuthorizationHeader, 1, 8192)
		if err != nil {
			return TelemetryExporterAuthConfig{}, err
		}
		result.AuthorizationHeader = header
		return result, nil
	}
	if a.AuthorizationHeader != nil {
		header, err := optionalTrimmedString("telemetry.exporter.auth.authorizationHeader", a.AuthorizationHeader, 8192)
		if err != nil {
			return TelemetryExporterAuthConfig{}, err
		}
		result.AuthorizationHeader = header
	}
	return result, nil
}

func (t *bootstrapTelemetryExporterTLS) toTelemetryExporterTLSConfig(enabled bool) (TelemetryExporterTLSConfig, error) {
	result := TelemetryExporterTLSConfig{}
	if t == nil {
		if enabled {
			return TelemetryExporterTLSConfig{}, missingBootstrapFieldError("telemetry.exporter.tls")
		}
		return result, nil
	}
	insecureSkipVerify, err := requiredBool("telemetry.exporter.tls.insecureSkipVerify", t.InsecureSkipVerify)
	if err != nil {
		return TelemetryExporterTLSConfig{}, err
	}
	result.InsecureSkipVerify = insecureSkipVerify
	if t.CAFile != nil {
		caFile, err := requiredTrimmedString("telemetry.exporter.tls.caFile", t.CAFile, 1, 4096)
		if err != nil {
			return TelemetryExporterTLSConfig{}, err
		}
		if !filepath.IsAbs(caFile) {
			return TelemetryExporterTLSConfig{}, fmt.Errorf("bootstrap config field telemetry.exporter.tls.caFile must be an absolute container-readable trust-root path")
		}
		result.CAFile = filepath.Clean(caFile)
	}
	return result, nil
}

func (s *bootstrapTelemetrySignal) toTelemetrySignalConfig(path string, telemetryEnabled bool) (TelemetrySignalConfig, error) {
	result := TelemetrySignalConfig{}
	if s == nil {
		if telemetryEnabled {
			return TelemetrySignalConfig{}, missingBootstrapFieldError(path)
		}
		return result, nil
	}
	enabled, err := requiredBool(path+".enabled", s.Enabled)
	if err != nil {
		return TelemetrySignalConfig{}, err
	}
	result.Enabled = enabled
	return result, nil
}

func (t *bootstrapTelemetryTraces) toTelemetryTracesConfig(telemetryEnabled bool) (TelemetryTracesConfig, error) {
	result := defaultTelemetryConfig().Traces
	if t == nil {
		if telemetryEnabled {
			return TelemetryTracesConfig{}, missingBootstrapFieldError("telemetry.traces")
		}
		return result, nil
	}
	enabled, err := requiredBool("telemetry.traces.enabled", t.Enabled)
	if err != nil {
		return TelemetryTracesConfig{}, err
	}
	result.Enabled = enabled
	if t.SamplingRatio != nil {
		samplingRatio, err := requiredFloat64Range("telemetry.traces.samplingRatio", t.SamplingRatio, 0, 1)
		if err != nil {
			return TelemetryTracesConfig{}, err
		}
		result.SamplingRatio = samplingRatio
	} else if telemetryEnabled && enabled {
		return TelemetryTracesConfig{}, missingBootstrapFieldError("telemetry.traces.samplingRatio")
	}
	return result, nil
}

func (t bootstrapRuntimeTransport) toRuntimeTransportConfig() (RuntimeTransportConfig, error) {
	maxIdleConns, err := requiredIntMin("transport.maxIdleConns", t.MaxIdleConns, 1)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	maxIdleConnsPerHost, err := requiredIntMin("transport.maxIdleConnsPerHost", t.MaxIdleConnsPerHost, 1)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	maxConnsPerHost, err := requiredIntMin("transport.maxConnsPerHost", t.MaxConnsPerHost, 0)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	requestTimeout, err := parseDurationField("transport.requestTimeout", t.RequestTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	if requestTimeout <= 0 {
		return RuntimeTransportConfig{}, fmt.Errorf("bootstrap config field transport.requestTimeout must be greater than zero")
	}
	idleConnTimeout, err := parseDurationField("transport.idleConnTimeout", t.IdleConnTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	responseHeaderTimeout, err := parseDurationField("transport.responseHeaderTimeout", t.ResponseHeaderTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	tlsHandshakeTimeout, err := parseDurationField("transport.tlsHandshakeTimeout", t.TLSHandshakeTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	expectContinueTimeout, err := parseDurationField("transport.expectContinueTimeout", t.ExpectContinueTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	return RuntimeTransportConfig{
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		MaxConnsPerHost:       maxConnsPerHost,
		RequestTimeout:        requestTimeout,
		IdleConnTimeout:       idleConnTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
	}, nil
}

func (s bootstrapRuntimeSideEffects) toRuntimeSideEffectsConfig() (RuntimeSideEffectsConfig, error) {
	attemptTimeout, err := parseDurationField("sideEffects.attemptTimeout", s.AttemptTimeout)
	if err != nil {
		return RuntimeSideEffectsConfig{}, err
	}
	if attemptTimeout <= 0 {
		return RuntimeSideEffectsConfig{}, fmt.Errorf("bootstrap config field sideEffects.attemptTimeout must be greater than zero")
	}
	return RuntimeSideEffectsConfig{AttemptTimeout: attemptTimeout}, nil
}

func buildSeededBootstrapDocument(settings Settings, now time.Time) (bootstrapConfigDocument, error) {
	postgresPoolsBudget := settings.PostgresPoolsBudgetOrDefault()
	managementAdmissionBudget := settings.ManagementAdmissionBudget()
	runtimeTransport := settings.RuntimeTransport()
	runtimeSideEffects := settings.RuntimeSideEffects()
	corsAllowedOrigins := settings.CORSAllowedOriginsList()
	databaseURL := strings.TrimSpace(settings.DatabaseURL)
	if databaseURL == "" {
		return bootstrapConfigDocument{}, fmt.Errorf("seeded database URL is empty")
	}
	runtimeSecretEncryptionKey := strings.TrimSpace(settings.SecretEncryptionKey)
	if runtimeSecretEncryptionKey == "" {
		return bootstrapConfigDocument{}, fmt.Errorf("seeded runtime secret encryption key is empty")
	}
	authJWTSecret := strings.TrimSpace(settings.AuthJWTSecret)
	if authJWTSecret == "" {
		return bootstrapConfigDocument{}, fmt.Errorf("seeded auth JWT signing key is empty")
	}
	bundleEncryptionKey := strings.TrimSpace(settings.ConfigBundleEncryptionKey)
	if bundleEncryptionKey == "" {
		bundleEncryptionKey = runtimeSecretEncryptionKey
	}
	timestamp := now.UTC().Format(time.RFC3339)

	return bootstrapConfigDocument{
		Meta: &bootstrapMeta{
			SchemaVersion: intPointer(bootstrapConfigSchemaVersion),
			Revision:      intPointer(1),
			CreatedAt:     stringPointer(timestamp),
			UpdatedAt:     stringPointer(timestamp),
		},
		Server: &bootstrapServer{
			Host: stringPointer(strings.TrimSpace(settings.Host)),
			Port: intPointer(settings.Port),
		},
		Database: &bootstrapDatabase{
			URL: stringPointer(databaseURL),
			Pools: &bootstrapDatabasePools{
				TotalMaxConns:    intPointer(int(postgresPoolsBudget.TotalMaxConns)),
				Management:       bootstrapDatabasePoolFromBudget(postgresPoolsBudget.Management),
				RuntimeExecution: bootstrapDatabasePoolFromBudget(postgresPoolsBudget.RuntimeExecution),
				RuntimeTelemetry: bootstrapDatabasePoolFromBudget(postgresPoolsBudget.RuntimeTelemetry),
				RuntimeFeedback:  bootstrapDatabasePoolFromBudget(postgresPoolsBudget.RuntimeFeedback),
				Realtime:         bootstrapDatabasePoolFromBudget(postgresPoolsBudget.Realtime),
				CacheRefresh:     bootstrapDatabasePoolFromBudget(postgresPoolsBudget.CacheRefresh),
				BackgroundJobs:   bootstrapDatabasePoolFromBudget(postgresPoolsBudget.BackgroundJobs),
			},
			ManagementAdmission: &bootstrapManagementAdmission{
				M2MaxConcurrent: intPointer(int(managementAdmissionBudget.M2MaxConcurrent)),
				M3MaxConcurrent: intPointer(int(managementAdmissionBudget.M3MaxConcurrent)),
			},
		},
		Runtime: &bootstrapRuntime{
			SecretEncryptionKey: stringPointer(runtimeSecretEncryptionKey),
			Transport: &bootstrapRuntimeTransport{
				MaxIdleConns:          intPointer(runtimeTransport.MaxIdleConns),
				MaxIdleConnsPerHost:   intPointer(runtimeTransport.MaxIdleConnsPerHost),
				MaxConnsPerHost:       intPointer(runtimeTransport.MaxConnsPerHost),
				RequestTimeout:        stringPointer(bootstrapRequestTimeoutString(runtimeTransport.RequestTimeout)),
				IdleConnTimeout:       stringPointer(bootstrapDurationString(runtimeTransport.IdleConnTimeout)),
				ResponseHeaderTimeout: stringPointer(bootstrapDurationString(runtimeTransport.ResponseHeaderTimeout)),
				TLSHandshakeTimeout:   stringPointer(bootstrapDurationString(runtimeTransport.TLSHandshakeTimeout)),
				ExpectContinueTimeout: stringPointer(bootstrapDurationString(runtimeTransport.ExpectContinueTimeout)),
			},
			SideEffects: &bootstrapRuntimeSideEffects{
				AttemptTimeout: stringPointer(bootstrapDurationString(runtimeSideEffects.AttemptTimeout)),
			},
		},
		HTTP: &bootstrapHTTP{
			CORSAllowedOrigins: &corsAllowedOrigins,
		},
		Auth: &bootstrapAuth{
			JWTSigningKey:          stringPointer(authJWTSecret),
			AccessTokenTTLSeconds:  intPointer(settings.AuthAccessTokenTTLSeconds),
			RefreshTokenTTLSeconds: intPointer(settings.AuthRefreshTokenTTLSeconds),
			ResetCodeTTLSeconds:    intPointer(settings.AuthResetCodeTTLSeconds),
			AccessCookieName:       stringPointer(strings.TrimSpace(settings.AuthCookieName)),
			RefreshCookieName:      stringPointer(strings.TrimSpace(settings.AuthRefreshCookieName)),
			CookieSecure:           boolPointer(settings.AuthCookieSecure),
		},
		Mail: &bootstrapMail{
			Enabled: boolPointer(false),
		},
		Telemetry: &bootstrapTelemetry{
			Enabled: boolPointer(false),
		},
		StateTransfer: &bootstrapStateTransfer{
			BundleEncryptionKey: stringPointer(bundleEncryptionKey),
		},
	}, nil
}

func bootstrapDatabasePoolFromBudget(budget DatabasePoolBudget) *bootstrapDatabasePool {
	return &bootstrapDatabasePool{MaxConns: intPointer(int(budget.MaxConns)), MinIdleConns: intPointer(int(budget.MinIdleConns))}
}

func bootstrapRequestTimeoutString(timeout time.Duration) string {
	return bootstrapDurationString(timeout)
}

func bootstrapDurationString(duration time.Duration) string {
	switch duration {
	case defaultRuntimeTransportRequestTimeout:
		return "300s"
	case defaultRuntimeTransportIdleConnTimeout:
		return "90s"
	case defaultRuntimeTransportResponseHeaderTimeout:
		return "0s"
	case defaultRuntimeTransportTLSHandshakeTimeout:
		return "10s"
	case defaultRuntimeTransportExpectContinueTimeout:
		return "1s"
	default:
		return duration.String()
	}
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

func missingBootstrapFieldError(path string) error {
	return fmt.Errorf("bootstrap config field %s is required", path)
}

func requiredTrimmedString(path string, value *string, minLength int, maxLength int) (string, error) {
	if value == nil {
		return "", missingBootstrapFieldError(path)
	}
	trimmed := strings.TrimSpace(*value)
	if len(trimmed) < minLength {
		return "", fmt.Errorf("bootstrap config field %s must be at least %d characters", path, minLength)
	}
	if maxLength > 0 && len(trimmed) > maxLength {
		return "", fmt.Errorf("bootstrap config field %s must be at most %d characters", path, maxLength)
	}
	return trimmed, nil
}

func requiredEnumString(path string, value *string, allowed []string) (string, error) {
	resolved, err := requiredTrimmedString(path, value, 1, 0)
	if err != nil {
		return "", err
	}
	if slices.Contains(allowed, resolved) {
		return resolved, nil
	}
	return "", fmt.Errorf("bootstrap config field %s must be one of %q", path, allowed)
}

func optionalTrimmedString(path string, value *string, maxLength int) (string, error) {
	if value == nil {
		return "", nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return "", nil
	}
	if maxLength > 0 && len(trimmed) > maxLength {
		return "", fmt.Errorf("bootstrap config field %s must be at most %d characters", path, maxLength)
	}
	return trimmed, nil
}

func requiredMailAddress(path string, value *string) (string, error) {
	resolved, err := requiredTrimmedString(path, value, 1, 320)
	if err != nil {
		return "", err
	}
	if _, err := mail.ParseAddress(resolved); err != nil {
		return "", fmt.Errorf("bootstrap config field %s must be a valid email address", path)
	}
	return resolved, nil
}

func optionalMailAddress(path string, value *string) (string, error) {
	resolved, err := optionalTrimmedString(path, value, 320)
	if err != nil || resolved == "" {
		return resolved, err
	}
	if _, err := mail.ParseAddress(resolved); err != nil {
		return "", fmt.Errorf("bootstrap config field %s must be a valid email address", path)
	}
	return resolved, nil
}

func allowedMailSMTPModes() []string {
	return []string{string(MailSMTPModeStartTLSRequired), string(MailSMTPModeImplicitTLS), string(MailSMTPModePlaintextLocalOnly)}
}

func allowedMailSMTPAuthModes() []string {
	return []string{string(MailSMTPAuthNone), string(MailSMTPAuthPlain)}
}

func allowedTelemetryExporterProtocols() []string {
	return []string{string(TelemetryExporterProtocolGRPC), string(TelemetryExporterProtocolHTTPProtobuf)}
}

func allowedTelemetryExporterCompressions() []string {
	return []string{string(TelemetryExporterCompressionNone), string(TelemetryExporterCompressionGzip)}
}

func allowedTelemetryExporterAuthModes() []string {
	return []string{string(TelemetryExporterAuthModeNone), string(TelemetryExporterAuthModeAuthorizationHeader)}
}

func normalizedMailSMTPMode(value *string) MailSMTPMode {
	if value == nil {
		return ""
	}
	return MailSMTPMode(strings.TrimSpace(*value))
}

func normalizedMailSMTPAuth(value *string) MailSMTPAuth {
	if value == nil {
		return MailSMTPAuthNone
	}
	return MailSMTPAuth(strings.TrimSpace(*value))
}

func hasNonEmptyString(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func isLocalSMTPHost(host string) bool {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(strings.TrimSuffix(trimmed, "."))
	if lower == "localhost" {
		return true
	}
	parsed := net.ParseIP(trimmed)
	return parsed != nil && parsed.IsLoopback()
}

func requiredDateTime(path string, value *string) (time.Time, error) {
	resolved, err := requiredTrimmedString(path, value, 1, 0)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339, resolved)
	if err != nil {
		return time.Time{}, fmt.Errorf("bootstrap config field %s must be a valid RFC3339 date-time", path)
	}
	return parsed, nil
}

func requiredIntConst(path string, value *int, expected int) (int, error) {
	resolved, err := requiredIntRange(path, value, expected, expected)
	if err != nil {
		return 0, err
	}
	return resolved, nil
}

func requiredIntMin(path string, value *int, minimum int) (int, error) {
	if value == nil {
		return 0, missingBootstrapFieldError(path)
	}
	if *value < minimum {
		return 0, fmt.Errorf("bootstrap config field %s must be greater than or equal to %d", path, minimum)
	}
	return *value, nil
}

func requiredIntRange(path string, value *int, minimum int, maximum int) (int, error) {
	if value == nil {
		return 0, missingBootstrapFieldError(path)
	}
	if *value < minimum || *value > maximum {
		return 0, fmt.Errorf("bootstrap config field %s must be between %d and %d", path, minimum, maximum)
	}
	return *value, nil
}

func requiredFloat64Range(path string, value *float64, minimum float64, maximum float64) (float64, error) {
	if value == nil {
		return 0, missingBootstrapFieldError(path)
	}
	if *value < minimum || *value > maximum {
		return 0, fmt.Errorf("bootstrap config field %s must be between %g and %g", path, minimum, maximum)
	}
	return *value, nil
}

func requiredBool(path string, value *bool) (bool, error) {
	if value == nil {
		return false, missingBootstrapFieldError(path)
	}
	return *value, nil
}

func requiredAbsoluteURIs(path string, value *[]string) ([]string, error) {
	if value == nil {
		return nil, missingBootstrapFieldError(path)
	}
	resolved := make([]string, 0, len(*value))
	seen := make(map[string]struct{}, len(*value))
	for index, rawValue := range *value {
		trimmed := strings.TrimSpace(rawValue)
		if trimmed == "" {
			return nil, fmt.Errorf("bootstrap config field %s[%d] must be a non-empty absolute URI", path, index)
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
			return nil, fmt.Errorf("bootstrap config field %s[%d] must be a non-empty absolute URI", path, index)
		}
		if _, exists := seen[trimmed]; exists {
			return nil, fmt.Errorf("bootstrap config field %s contains duplicate URI %q", path, trimmed)
		}
		seen[trimmed] = struct{}{}
		resolved = append(resolved, trimmed)
	}
	return resolved, nil
}

func parseDurationField(path string, value *string) (time.Duration, error) {
	resolved, err := requiredTrimmedString(path, value, 1, 0)
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(resolved)
	if err != nil {
		return 0, fmt.Errorf("bootstrap config field %s must parse as a Go duration", path)
	}
	return parsed, nil
}

func hasBootstrapField(value json.RawMessage) bool {
	return len(bytes.TrimSpace(value)) > 0
}

type bootstrapUnsupportedFieldProbe struct {
	SecretPayload json.RawMessage            `json:"secretPayload"`
	Database      map[string]json.RawMessage `json:"database"`
	Auth          map[string]json.RawMessage `json:"auth"`
	StateTransfer map[string]json.RawMessage `json:"stateTransfer"`
}

func detectUnsupportedBootstrapFormat(raw []byte) error {
	var probe bootstrapUnsupportedFieldProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	unsupportedFields := make([]string, 0, 4)
	if hasBootstrapField(probe.SecretPayload) {
		unsupportedFields = append(unsupportedFields, "secretPayload")
	}
	if hasBootstrapMapField(probe.Database, "urlSecretRef") {
		unsupportedFields = append(unsupportedFields, "database.urlSecretRef")
	}
	if hasBootstrapMapField(probe.Auth, "jwtSigningKeySecretRef") {
		unsupportedFields = append(unsupportedFields, "auth.jwtSigningKeySecretRef")
	}
	if hasBootstrapMapField(probe.StateTransfer, "bundleEncryptionKeySecretRef") {
		unsupportedFields = append(unsupportedFields, "stateTransfer.bundleEncryptionKeySecretRef")
	}
	if len(unsupportedFields) == 0 {
		return nil
	}
	return fmt.Errorf("bootstrap config uses unsupported legacy encrypted format fields: %s", strings.Join(unsupportedFields, ", "))
}

func hasBootstrapMapField(fields map[string]json.RawMessage, name string) bool {
	if fields == nil {
		return false
	}
	value, ok := fields[name]
	if !ok {
		return false
	}
	return hasBootstrapField(value)
}
