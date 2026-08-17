package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

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

func allowedTelemetryExporterProtocols() []string {
	return []string{string(TelemetryExporterProtocolGRPC), string(TelemetryExporterProtocolHTTPProtobuf)}
}

func allowedTelemetryExporterCompressions() []string {
	return []string{string(TelemetryExporterCompressionNone), string(TelemetryExporterCompressionGzip)}
}

func allowedTelemetryExporterAuthModes() []string {
	return []string{string(TelemetryExporterAuthModeNone), string(TelemetryExporterAuthModeAuthorizationHeader)}
}

func (t *bootstrapTelemetry) validate() error {
	_, err := t.toTelemetryConfig()
	return err
}
