package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	BootstrapConfigPathEnv = "PRISM_CONFIG_PATH"

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

type bootstrapConfigDocument struct {
	Meta          *bootstrapMeta          `json:"meta"`
	Server        *bootstrapServer        `json:"server"`
	Database      *bootstrapDatabase      `json:"database"`
	Runtime       *bootstrapRuntime       `json:"runtime"`
	HTTP          *bootstrapHTTP          `json:"http"`
	Auth          *bootstrapAuth          `json:"auth"`
	StateTransfer *bootstrapStateTransfer `json:"stateTransfer"`
}

type bootstrapMeta struct {
	SchemaVersion *int    `json:"schemaVersion"`
	Revision      *int    `json:"revision"`
	CreatedAt     *string `json:"createdAt"`
	UpdatedAt     *string `json:"updatedAt"`
}

type bootstrapServer struct {
	Host        *string `json:"host"`
	Port        *int    `json:"port"`
	DocsEnabled *bool   `json:"docsEnabled"`
}

type bootstrapDatabase struct {
	URL                 *string                       `json:"url"`
	RuntimePool         *bootstrapDatabasePool        `json:"runtimePool"`
	ManagementPool      *bootstrapDatabasePool        `json:"managementPool"`
	ManagementAdmission *bootstrapManagementAdmission `json:"managementAdmission"`
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
	BufferingMode       *string                    `json:"bufferingMode"`
	SecretEncryptionKey *string                    `json:"secretEncryptionKey"`
	Transport           *bootstrapRuntimeTransport `json:"transport"`
}

type bootstrapRuntimeTransport struct {
	MaxIdleConns          *int    `json:"maxIdleConns"`
	MaxIdleConnsPerHost   *int    `json:"maxIdleConnsPerHost"`
	MaxConnsPerHost       *int    `json:"maxConnsPerHost"`
	IdleConnTimeout       *string `json:"idleConnTimeout"`
	ResponseHeaderTimeout *string `json:"responseHeaderTimeout"`
	TLSHandshakeTimeout   *string `json:"tlsHandshakeTimeout"`
	ExpectContinueTimeout *string `json:"expectContinueTimeout"`
}

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

	payload, settings, err := m.seedPayloadFromEnv()
	if err != nil {
		return Settings{}, fmt.Errorf("seed bootstrap config %q from legacy env: %w", normalizedPath, err)
	}
	written, err := m.WriteAtomicallyIfAbsent(normalizedPath, payload)
	if err != nil {
		return Settings{}, fmt.Errorf("seed bootstrap config %q from legacy env: %w", normalizedPath, err)
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

func (m BootstrapConfigManager) Parse(raw []byte) (Settings, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return Settings{}, fmt.Errorf("bootstrap config is empty")
	}
	if err := detectUnsupportedBootstrapFormat(raw); err != nil {
		return Settings{}, err
	}

	document, err := decodeBootstrapConfig(raw)
	if err != nil {
		return Settings{}, err
	}
	if err := document.validateSchema(); err != nil {
		return Settings{}, err
	}
	if err := document.validateSemantics(); err != nil {
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

func resolveBootstrapExternalInputsFromEnv() (string, error) {
	configPath := strings.TrimSpace(os.Getenv(BootstrapConfigPathEnv))
	if configPath == "" {
		return "", fmt.Errorf("%s is required", BootstrapConfigPathEnv)
	}
	return configPath, nil
}

func (m BootstrapConfigManager) seedPayloadFromEnv() ([]byte, Settings, error) {
	document, err := buildSeededBootstrapDocument(loadSeedSettingsFromEnv(), m.resolvedTimeNow()().UTC())
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
	_, err := requiredBool("server.docsEnabled", s.DocsEnabled)
	return err
}

func (d bootstrapDatabase) validate() error {
	if _, err := requiredTrimmedString("database.url", d.URL, 1, 0); err != nil {
		return err
	}
	if d.RuntimePool == nil {
		return missingBootstrapFieldError("database.runtimePool")
	}
	if d.ManagementPool == nil {
		return missingBootstrapFieldError("database.managementPool")
	}
	if d.ManagementAdmission == nil {
		return missingBootstrapFieldError("database.managementAdmission")
	}
	if err := d.RuntimePool.validate("database.runtimePool"); err != nil {
		return err
	}
	if err := d.ManagementPool.validate("database.managementPool"); err != nil {
		return err
	}
	return d.ManagementAdmission.validate()
}

func (p bootstrapDatabasePool) validate(path string) error {
	if _, err := requiredIntRange(path+".maxConns", p.MaxConns, 1, math.MaxInt32); err != nil {
		return err
	}
	if _, err := requiredIntRange(path+".minIdleConns", p.MinIdleConns, 0, math.MaxInt32); err != nil {
		return err
	}
	return nil
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
	if _, err := requiredEnumString("runtime.bufferingMode", r.BufferingMode, []string{string(RuntimeBufferingModeBuffered), string(RuntimeBufferingModeStreaming)}); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("runtime.secretEncryptionKey", r.SecretEncryptionKey, 1, 0); err != nil {
		return err
	}
	if r.Transport == nil {
		return missingBootstrapFieldError("runtime.transport")
	}
	return r.Transport.validate()
}

func (t bootstrapRuntimeTransport) validate() error {
	if _, err := requiredIntMin("runtime.transport.maxIdleConns", t.MaxIdleConns, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("runtime.transport.maxIdleConnsPerHost", t.MaxIdleConnsPerHost, 1); err != nil {
		return err
	}
	if _, err := requiredIntMin("runtime.transport.maxConnsPerHost", t.MaxConnsPerHost, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("runtime.transport.idleConnTimeout", t.IdleConnTimeout, 1, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("runtime.transport.responseHeaderTimeout", t.ResponseHeaderTimeout, 1, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("runtime.transport.tlsHandshakeTimeout", t.TLSHandshakeTimeout, 1, 0); err != nil {
		return err
	}
	if _, err := requiredTrimmedString("runtime.transport.expectContinueTimeout", t.ExpectContinueTimeout, 1, 0); err != nil {
		return err
	}
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

func (s bootstrapStateTransfer) validate() error {
	_, err := requiredTrimmedString("stateTransfer.bundleEncryptionKey", s.BundleEncryptionKey, 1, 0)
	return err
}

func (d bootstrapConfigDocument) validateSemantics() error {
	for _, field := range []struct {
		path  string
		value *string
	}{
		{path: "runtime.transport.idleConnTimeout", value: d.Runtime.Transport.IdleConnTimeout},
		{path: "runtime.transport.responseHeaderTimeout", value: d.Runtime.Transport.ResponseHeaderTimeout},
		{path: "runtime.transport.tlsHandshakeTimeout", value: d.Runtime.Transport.TLSHandshakeTimeout},
		{path: "runtime.transport.expectContinueTimeout", value: d.Runtime.Transport.ExpectContinueTimeout},
	} {
		if _, err := parseDurationField(field.path, field.value); err != nil {
			return err
		}
	}
	runtimeMaxConns, _ := requiredIntRange("database.runtimePool.maxConns", d.Database.RuntimePool.MaxConns, 1, math.MaxInt32)
	runtimeMinIdleConns, _ := requiredIntRange("database.runtimePool.minIdleConns", d.Database.RuntimePool.MinIdleConns, 0, math.MaxInt32)
	if runtimeMinIdleConns > runtimeMaxConns {
		return fmt.Errorf("bootstrap config field database.runtimePool.minIdleConns must be less than or equal to database.runtimePool.maxConns")
	}
	managementMaxConns, _ := requiredIntRange("database.managementPool.maxConns", d.Database.ManagementPool.MaxConns, 1, math.MaxInt32)
	managementMinIdleConns, _ := requiredIntRange("database.managementPool.minIdleConns", d.Database.ManagementPool.MinIdleConns, 0, math.MaxInt32)
	if managementMinIdleConns > managementMaxConns {
		return fmt.Errorf("bootstrap config field database.managementPool.minIdleConns must be less than or equal to database.managementPool.maxConns")
	}
	m2MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m2MaxConcurrent", d.Database.ManagementAdmission.M2MaxConcurrent, 1)
	m3MaxConcurrent, _ := requiredIntMin("database.managementAdmission.m3MaxConcurrent", d.Database.ManagementAdmission.M3MaxConcurrent, 1)
	if m3MaxConcurrent > m2MaxConcurrent {
		return fmt.Errorf("bootstrap config field database.managementAdmission.m3MaxConcurrent must be less than or equal to database.managementAdmission.m2MaxConcurrent")
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
	docsEnabled, err := requiredBool("server.docsEnabled", d.Server.DocsEnabled)
	if err != nil {
		return Settings{}, err
	}
	databaseURL, err := requiredTrimmedString("database.url", d.Database.URL, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	runtimeBufferingMode, err := requiredEnumString("runtime.bufferingMode", d.Runtime.BufferingMode, []string{string(RuntimeBufferingModeBuffered), string(RuntimeBufferingModeStreaming)})
	if err != nil {
		return Settings{}, err
	}
	runtimeSecretEncryptionKey, err := requiredTrimmedString("runtime.secretEncryptionKey", d.Runtime.SecretEncryptionKey, 1, 0)
	if err != nil {
		return Settings{}, err
	}
	runtimeTransport, err := d.Runtime.Transport.toRuntimeTransportConfig()
	if err != nil {
		return Settings{}, err
	}
	runtimeMaxConns, _ := requiredIntRange("database.runtimePool.maxConns", d.Database.RuntimePool.MaxConns, 1, math.MaxInt32)
	runtimeMinIdleConns, _ := requiredIntRange("database.runtimePool.minIdleConns", d.Database.RuntimePool.MinIdleConns, 0, math.MaxInt32)
	managementMaxConns, _ := requiredIntRange("database.managementPool.maxConns", d.Database.ManagementPool.MaxConns, 1, math.MaxInt32)
	managementMinIdleConns, _ := requiredIntRange("database.managementPool.minIdleConns", d.Database.ManagementPool.MinIdleConns, 0, math.MaxInt32)
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

	appEnv := EnvironmentProduction
	if docsEnabled {
		appEnv = EnvironmentDevelopment
	}

	return Settings{
		Host:                             host,
		Port:                             port,
		AppEnv:                           appEnv,
		DatabaseURL:                      databaseURL,
		RuntimeTelemetryMode:             RuntimeTelemetryModeDurableOutbox,
		RuntimeBufferingMode:             RuntimeBufferingMode(runtimeBufferingMode),
		RuntimeTransportConfig:           runtimeTransport,
		RuntimeDatabasePoolBudget:        DatabasePoolBudget{MaxConns: int32(runtimeMaxConns), MinIdleConns: int32(runtimeMinIdleConns)},
		ManagementDatabasePoolBudget:     DatabasePoolBudget{MaxConns: int32(managementMaxConns), MinIdleConns: int32(managementMinIdleConns)},
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
	}, nil
}

func (t bootstrapRuntimeTransport) toRuntimeTransportConfig() (RuntimeTransportConfig, error) {
	maxIdleConns, err := requiredIntMin("runtime.transport.maxIdleConns", t.MaxIdleConns, 1)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	maxIdleConnsPerHost, err := requiredIntMin("runtime.transport.maxIdleConnsPerHost", t.MaxIdleConnsPerHost, 1)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	maxConnsPerHost, err := requiredIntMin("runtime.transport.maxConnsPerHost", t.MaxConnsPerHost, 0)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	idleConnTimeout, err := parseDurationField("runtime.transport.idleConnTimeout", t.IdleConnTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	responseHeaderTimeout, err := parseDurationField("runtime.transport.responseHeaderTimeout", t.ResponseHeaderTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	tlsHandshakeTimeout, err := parseDurationField("runtime.transport.tlsHandshakeTimeout", t.TLSHandshakeTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	expectContinueTimeout, err := parseDurationField("runtime.transport.expectContinueTimeout", t.ExpectContinueTimeout)
	if err != nil {
		return RuntimeTransportConfig{}, err
	}
	return RuntimeTransportConfig{
		MaxIdleConns:          maxIdleConns,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		MaxConnsPerHost:       maxConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
	}, nil
}

func buildSeededBootstrapDocument(settings Settings, now time.Time) (bootstrapConfigDocument, error) {
	runtimeDatabaseBudget := settings.RuntimeDatabaseBudget()
	managementDatabaseBudget := settings.ManagementDatabaseBudget()
	managementAdmissionBudget := settings.ManagementAdmissionBudget()
	runtimeTransport := settings.RuntimeTransport()
	corsAllowedOrigins := settings.CORSAllowedOriginsList()
	databaseURL, err := requiredSeedValue("DATABASE_URL", settings.DatabaseURL)
	if err != nil {
		return bootstrapConfigDocument{}, err
	}
	runtimeSecretEncryptionKey, err := requiredSeedValue("SECRET_ENCRYPTION_KEY", settings.SecretEncryptionKey)
	if err != nil {
		return bootstrapConfigDocument{}, err
	}
	authJWTSecret, err := requiredSeedValue("AUTH_JWT_SECRET", settings.AuthJWTSecret)
	if err != nil {
		return bootstrapConfigDocument{}, err
	}
	bundleEncryptionKey := strings.TrimSpace(settings.ConfigBundleEncryptionKey)
	if bundleEncryptionKey == "" {
		bundleEncryptionKey = runtimeSecretEncryptionKey
	}
	timestamp := now.UTC().Format(time.RFC3339)
	bufferingMode := string(settings.ResolvedRuntimeBufferingMode())

	return bootstrapConfigDocument{
		Meta: &bootstrapMeta{
			SchemaVersion: intPointer(bootstrapConfigSchemaVersion),
			Revision:      intPointer(1),
			CreatedAt:     stringPointer(timestamp),
			UpdatedAt:     stringPointer(timestamp),
		},
		Server: &bootstrapServer{
			Host:        stringPointer(strings.TrimSpace(settings.Host)),
			Port:        intPointer(settings.Port),
			DocsEnabled: boolPointer(settings.DocsEnabled()),
		},
		Database: &bootstrapDatabase{
			URL: stringPointer(databaseURL),
			RuntimePool: &bootstrapDatabasePool{
				MaxConns:     intPointer(int(runtimeDatabaseBudget.MaxConns)),
				MinIdleConns: intPointer(int(runtimeDatabaseBudget.MinIdleConns)),
			},
			ManagementPool: &bootstrapDatabasePool{
				MaxConns:     intPointer(int(managementDatabaseBudget.MaxConns)),
				MinIdleConns: intPointer(int(managementDatabaseBudget.MinIdleConns)),
			},
			ManagementAdmission: &bootstrapManagementAdmission{
				M2MaxConcurrent: intPointer(int(managementAdmissionBudget.M2MaxConcurrent)),
				M3MaxConcurrent: intPointer(int(managementAdmissionBudget.M3MaxConcurrent)),
			},
		},
		Runtime: &bootstrapRuntime{
			BufferingMode:       stringPointer(bufferingMode),
			SecretEncryptionKey: stringPointer(runtimeSecretEncryptionKey),
			Transport: &bootstrapRuntimeTransport{
				MaxIdleConns:          intPointer(runtimeTransport.MaxIdleConns),
				MaxIdleConnsPerHost:   intPointer(runtimeTransport.MaxIdleConnsPerHost),
				MaxConnsPerHost:       intPointer(runtimeTransport.MaxConnsPerHost),
				IdleConnTimeout:       stringPointer(runtimeTransport.IdleConnTimeout.String()),
				ResponseHeaderTimeout: stringPointer(runtimeTransport.ResponseHeaderTimeout.String()),
				TLSHandshakeTimeout:   stringPointer(runtimeTransport.TLSHandshakeTimeout.String()),
				ExpectContinueTimeout: stringPointer(runtimeTransport.ExpectContinueTimeout.String()),
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
		StateTransfer: &bootstrapStateTransfer{
			BundleEncryptionKey: stringPointer(bundleEncryptionKey),
		},
	}, nil
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

func requiredSeedValue(envName string, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("legacy startup %s is required to seed bootstrap config", envName)
	}
	return trimmed, nil
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
