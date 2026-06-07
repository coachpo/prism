package configbundle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
)

type queryExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type endpointRow struct {
	ID       int
	Name     string
	BaseURL  string
	APIKey   string
	Position int
}

type pricingTemplateRow struct {
	ID                  int
	Name                string
	Description         *string
	PricingUnit         string
	PricingCurrencyCode string
	InputPrice          string
	OutputPrice         string
	CachedInputPrice    string
	CacheCreationPrice  string
	ReasoningPrice      string
	Version             int
}

type strategyRow struct {
	ID                                 int
	Name                               string
	LegacyStrategyType                 string
	FailureStatusCodes                 []int
	BanMode                            string
	RetryBaseDelayMS                   int
	RetryBackoffMultiplier             float64
	RetryJitterRatio                   float64
	RetryMaxDelayMS                    int
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BanDurationSeconds                 int
}

type modelRow struct {
	ID                                   int
	VendorID                             *int
	APIFamily                            string
	ModelID                              string
	DisplayName                          *string
	LoadbalanceStrategyID                *int
	ContextWindowTokens                  *int
	DefaultOutputTokenReserve            int
	MaxContextUtilization                float64
	PreferredContextUtilizationThreshold *float64
	FacadeEnabled                        bool
	FacadeSelectionPolicy                *string
	FacadeFallbackPolicy                 *string
	ContextOverflowPromotionTargetID     *string
	IsEnabled                            bool
}

type accessTargetRow struct {
	SourceModelConfigID int
	TargetType          string
	TargetModelID       *string
	TargetConnectionID  *int
	Position            int
	Weight              *int
	TargetPriority      *int
	IsEnabled           bool
}

type connectionRow struct {
	ID                                   int
	APIFamily                            string
	EndpointID                           int
	ContextWindowTokens                  *int
	DefaultOutputTokenReserve            int
	MaxContextUtilization                float64
	PreferredContextUtilizationThreshold *float64
	PricingTemplateID                    *int
	IsActive                             bool
	Priority                             int
	Name                                 *string
	AuthType                             *string
	CustomHeaders                        map[string]string
	OpenAIProbeEndpointVariant           *string
	QPSLimit                             *int
	MaxInFlightNonStream                 *int
	MaxInFlightStream                    *int
}

type vendorRow struct {
	ID                 int
	Key                string
	Name               string
	Description        *string
	IconKey            *string
	AuditEnabled       bool
	AuditCaptureBodies bool
}

type userSettingsRow struct {
	ReportCurrencyCode   string
	ReportCurrencySymbol string
	TimezonePreference   *string
}

type endpointFXMappingRow struct {
	ModelID      string
	ConnectionID int
	FXRate       string
}

type headerBlocklistRuleRow struct {
	Name      string
	MatchType string
	Pattern   string
	Enabled   bool
}

type userAgentClientRuleRow struct {
	Name    string
	Pattern string
	Enabled bool
}

func buildVendorCatalog(ctx context.Context, exec queryExecutor, exportTime time.Time) (vendorCatalogResponse, error) {
	vendors, err := listAllVendors(ctx, exec)
	if err != nil {
		return vendorCatalogResponse{}, err
	}
	return vendorCatalogResponse{
		Version:    canonicalVendorCatalogVersion,
		BundleKind: canonicalVendorCatalogKind,
		ExportedAt: exportTime,
		Vendors:    vendors,
	}, nil
}

func (s *Service) buildProfileBundle(ctx context.Context, exec queryExecutor, profileID int, exportTime time.Time, bundleSecretKeyID string, includeSecrets bool) (profileBundleResponse, error) {
	endpoints, err := listOrderedEndpoints(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	pricingTemplates, err := listPricingTemplates(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	strategies, err := listStrategies(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	connections, err := listConnections(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	models, err := listModels(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	userSettings, err := loadUserSettings(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	fxMappings, err := listEndpointFXMappings(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	headerRules, err := listProfileHeaderBlocklistRules(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	userAgentRules, err := listProfileUserAgentClientRules(ctx, exec, profileID)
	if err != nil {
		return profileBundleResponse{}, err
	}

	exportedEndpoints, endpointByID, secretEntries, err := s.buildEndpointExports(endpoints, includeSecrets)
	if err != nil {
		return profileBundleResponse{}, err
	}
	exportedPricingTemplates, pricingTemplateByID := buildPricingTemplateExports(pricingTemplates)
	exportedStrategies, strategyNameByID := buildLoadbalanceStrategyExports(strategies)
	exportedConnections, connectionRefByID, err := buildConnectionExports(connections, endpointByID, pricingTemplateByID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	exportedVendorRefs, exportedModels, err := buildModelExports(ctx, exec, models, strategyNameByID, connectionRefByID)
	if err != nil {
		return profileBundleResponse{}, err
	}
	exportedProfileSettings, err := buildProfileSettingsExport(userSettings, fxMappings, connectionRefByID)
	if err != nil {
		return profileBundleResponse{}, err
	}

	return profileBundleResponse{
		Version:               canonicalProfileBundleVersion,
		BundleKind:            canonicalProfileBundleKind,
		ExportedAt:            exportTime,
		VendorRefs:            exportedVendorRefs,
		Endpoints:             exportedEndpoints,
		PricingTemplates:      exportedPricingTemplates,
		Connections:           exportedConnections,
		LoadbalanceStrategies: exportedStrategies,
		Models:                exportedModels,
		ProfileSettings:       exportedProfileSettings,
		HeaderBlocklistRules:  buildHeaderBlocklistRuleExports(headerRules),
		UserAgentClientRules:  buildUserAgentClientRuleExports(userAgentRules),
		SecretPayload: secretPayloadExport{
			Kind:    "encrypted",
			Cipher:  bundleSecretCipher,
			KeyID:   bundleSecretKeyID,
			Entries: secretEntries,
		},
	}, nil
}

func (s *Service) buildEndpointExports(endpoints []endpointRow, includeSecrets bool) ([]endpointExport, map[int]endpointRow, []secretPayloadEntry, error) {
	endpointByID := make(map[int]endpointRow, len(endpoints))
	exportedEndpoints := make([]endpointExport, 0, len(endpoints))
	secretEntries := make([]secretPayloadEntry, 0, len(endpoints))
	for _, endpoint := range endpoints {
		endpointByID[endpoint.ID] = endpoint
		secretRef, entry, err := s.buildEndpointSecretExport(endpoint, includeSecrets)
		if err != nil {
			return nil, nil, nil, err
		}
		if entry != nil {
			secretEntries = append(secretEntries, *entry)
		}
		exportedEndpoints = append(exportedEndpoints, endpointExport{
			Name:            endpoint.Name,
			BaseURL:         endpoint.BaseURL,
			APIKeySecretRef: secretRef,
			Position:        endpoint.Position,
		})
	}
	return exportedEndpoints, endpointByID, secretEntries, nil
}

func (s *Service) buildEndpointSecretExport(endpoint endpointRow, includeSecrets bool) (*string, *secretPayloadEntry, error) {
	if !includeSecrets || !endpointdomain.HasAPIKey(endpoint.APIKey) {
		return nil, nil, nil
	}
	decryptedAPIKey, err := endpointdomain.DecryptSecret(endpoint.APIKey, s.secretEncryptionKey)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt endpoint %q secret: %w", endpoint.Name, err)
	}
	if strings.TrimSpace(decryptedAPIKey) == "" {
		return nil, nil, nil
	}
	ref := fmt.Sprintf("endpoint:%s:api_key", endpoint.Name)
	encryptedValue, err := s.bundleSecretEncrypter(decryptedAPIKey)
	if err != nil {
		return nil, nil, fmt.Errorf("encrypt bundle secret for endpoint %q: %w", endpoint.Name, err)
	}
	return &ref, &secretPayloadEntry{Ref: ref, Ciphertext: encryptedValue}, nil
}

func buildPricingTemplateExports(templates []pricingTemplateRow) ([]pricingTemplateExport, map[int]pricingTemplateRow) {
	pricingTemplateByID := make(map[int]pricingTemplateRow, len(templates))
	exportedTemplates := make([]pricingTemplateExport, 0, len(templates))
	for _, template := range templates {
		pricingTemplateByID[template.ID] = template
		exportedTemplates = append(exportedTemplates, pricingTemplateExport{
			Name:                template.Name,
			Description:         template.Description,
			PricingUnit:         template.PricingUnit,
			PricingCurrencyCode: template.PricingCurrencyCode,
			InputPrice:          template.InputPrice,
			OutputPrice:         template.OutputPrice,
			CachedInputPrice:    template.CachedInputPrice,
			CacheCreationPrice:  template.CacheCreationPrice,
			ReasoningPrice:      template.ReasoningPrice,
			Version:             template.Version,
		})
	}
	return exportedTemplates, pricingTemplateByID
}

func buildLoadbalanceStrategyExports(strategies []strategyRow) ([]loadbalanceStrategyExport, map[int]string) {
	strategyNameByID := make(map[int]string, len(strategies))
	exportedStrategies := make([]loadbalanceStrategyExport, 0, len(strategies))
	for _, strategy := range strategies {
		strategyNameByID[strategy.ID] = strategy.Name
		exportedStrategies = append(exportedStrategies, loadbalanceStrategyExport{
			Name:                               strategy.Name,
			LegacyStrategyType:                 stringPtr(strategy.LegacyStrategyType),
			FailureStatusCodes:                 append([]int(nil), strategy.FailureStatusCodes...),
			BanMode:                            stringPtr(strategy.BanMode),
			RetryBaseDelayMS:                   intPtr(strategy.RetryBaseDelayMS),
			RetryBackoffMultiplier:             float64Ptr(strategy.RetryBackoffMultiplier),
			RetryJitterRatio:                   float64Ptr(strategy.RetryJitterRatio),
			RetryMaxDelayMS:                    intPtr(strategy.RetryMaxDelayMS),
			CycleRetryAttemptLimit:             intPtr(strategy.CycleRetryAttemptLimit),
			BanCumulativeRetryAttemptThreshold: intPtr(strategy.BanCumulativeRetryAttemptThreshold),
			BanDurationSeconds:                 intPtr(strategy.BanDurationSeconds),
		})
	}
	return exportedStrategies, strategyNameByID
}

func buildModelExports(ctx context.Context, exec queryExecutor, models []modelRow, strategyNameByID map[int]string, connectionRefByID map[int]string) ([]vendorRefExport, []modelExport, error) {
	modelIDs, vendorIDs := profileModelExportIDs(models)
	vendorsByID, err := loadVendorsByIDs(ctx, exec, vendorIDs)
	if err != nil {
		return nil, nil, err
	}
	accessTargetsByModelID, err := listAccessTargetsByModelIDs(ctx, exec, modelIDs)
	if err != nil {
		return nil, nil, err
	}
	if err := validateExportedConnectionOwners(models, accessTargetsByModelID, connectionRefByID); err != nil {
		return nil, nil, err
	}

	exportedVendorRefs, err := buildVendorRefExports(vendorIDs, vendorsByID)
	if err != nil {
		return nil, nil, err
	}
	exportedModels := make([]modelExport, 0, len(models))
	for _, model := range models {
		exportedModel, err := buildModelExport(model, vendorsByID, strategyNameByID, accessTargetsByModelID[model.ID], connectionRefByID)
		if err != nil {
			return nil, nil, err
		}
		exportedModels = append(exportedModels, exportedModel)
	}
	return exportedVendorRefs, exportedModels, nil
}

func validateExportedConnectionOwners(models []modelRow, accessTargetsByModelID map[int][]accessTargetRow, connectionRefByID map[int]string) error {
	ownersByConnectionRef := map[string]connectionOwnerRef{}
	for _, model := range models {
		owner := connectionOwnerRef{ModelID: model.ModelID, DisplayName: model.DisplayName}
		for _, target := range accessTargetsByModelID[model.ID] {
			if !modelrouting.IsTerminalTargetType(target.TargetType) {
				continue
			}
			if target.TargetConnectionID == nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' connection access target must resolve to a connection_ref", model.ModelID)}
			}
			connectionRef, ok := connectionRefByID[*target.TargetConnectionID]
			if !ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' connection access target references unknown connection id %d", model.ModelID, *target.TargetConnectionID)}
			}
			if previousOwner, ok := ownersByConnectionRef[connectionRef]; ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: duplicateConnectionRefOwnerDetail(connectionRef, previousOwner, owner)}
			}
			ownersByConnectionRef[connectionRef] = owner
		}
	}
	return nil
}

func profileModelExportIDs(models []modelRow) ([]int, []int) {
	modelIDs := make([]int, 0, len(models))
	vendorIDs := make([]int, 0, len(models))
	seenVendorIDs := map[int]struct{}{}
	for _, model := range models {
		modelIDs = append(modelIDs, model.ID)
		if model.VendorID != nil {
			if _, ok := seenVendorIDs[*model.VendorID]; !ok {
				seenVendorIDs[*model.VendorID] = struct{}{}
				vendorIDs = append(vendorIDs, *model.VendorID)
			}
		}
	}
	return modelIDs, vendorIDs
}

func buildVendorRefExports(vendorIDs []int, vendorsByID map[int]vendorRow) ([]vendorRefExport, error) {
	exportedVendorRefs := make([]vendorRefExport, 0, len(vendorIDs))
	for _, vendorID := range vendorIDs {
		vendor, ok := vendorsByID[vendorID]
		if !ok {
			return nil, fmt.Errorf("load vendor %d for vendor refs", vendorID)
		}
		exportedVendorRefs = append(exportedVendorRefs, vendorRefExport{
			Key:             vendor.Key,
			NameHint:        vendor.Name,
			DescriptionHint: vendor.Description,
			IconKeyHint:     vendor.IconKey,
		})
	}
	return exportedVendorRefs, nil
}

func buildModelExport(model modelRow, vendorsByID map[int]vendorRow, strategyNameByID map[int]string, accessTargets []accessTargetRow, connectionRefByID map[int]string) (modelExport, error) {
	vendorKey, err := buildModelVendorKey(model, vendorsByID)
	if err != nil {
		return modelExport{}, err
	}
	strategyName, err := buildModelLoadbalanceStrategyName(model, strategyNameByID)
	if err != nil {
		return modelExport{}, err
	}
	exportedTargets, err := buildAccessTargetExports(model, accessTargets, connectionRefByID)
	if err != nil {
		return modelExport{}, err
	}
	return modelExport{
		VendorKey:                            vendorKey,
		APIFamily:                            model.APIFamily,
		ModelID:                              model.ModelID,
		DisplayName:                          model.DisplayName,
		LoadbalanceStrategyName:              strategyName,
		ContextWindowTokens:                  intPtrFromOptional(model.ContextWindowTokens),
		DefaultOutputTokenReserve:            intPtr(model.DefaultOutputTokenReserve),
		MaxContextUtilization:                float64Ptr(model.MaxContextUtilization),
		PreferredContextUtilizationThreshold: float64PtrFromOptional(model.PreferredContextUtilizationThreshold),
		FacadeEnabled:                        model.FacadeEnabled,
		FacadeSelectionPolicy:                model.FacadeSelectionPolicy,
		FacadeFallbackPolicy:                 model.FacadeFallbackPolicy,
		ContextOverflowPromotionTargetID:     model.ContextOverflowPromotionTargetID,
		IsEnabled:                            model.IsEnabled,
		AccessTargets:                        exportedTargets,
	}, nil
}

func buildModelVendorKey(model modelRow, vendorsByID map[int]vendorRow) (*string, error) {
	if model.VendorID == nil {
		return nil, nil
	}
	vendor, ok := vendorsByID[*model.VendorID]
	if !ok {
		return nil, fmt.Errorf("load vendor %d for model %q", *model.VendorID, model.ModelID)
	}
	value := vendor.Key
	return &value, nil
}

func buildModelLoadbalanceStrategyName(model modelRow, strategyNameByID map[int]string) (*string, error) {
	if model.LoadbalanceStrategyID == nil {
		return nil, nil
	}
	resolvedName, ok := strategyNameByID[*model.LoadbalanceStrategyID]
	if !ok {
		return nil, fmt.Errorf("load loadbalance strategy %d for model %q", *model.LoadbalanceStrategyID, model.ModelID)
	}
	return &resolvedName, nil
}

func buildAccessTargetExports(model modelRow, accessTargets []accessTargetRow, connectionRefByID map[int]string) ([]accessTargetExport, error) {
	exportedTargets := make([]accessTargetExport, 0, len(accessTargets))
	for _, target := range accessTargets {
		exportedTarget := accessTargetExport{Position: target.Position, IsEnabled: target.IsEnabled, TargetType: target.TargetType}
		switch {
		case modelrouting.IsTerminalTargetType(target.TargetType):
			if target.TargetConnectionID == nil {
				return nil, fmt.Errorf("load connection target for model %q", model.ModelID)
			}
			ref, ok := connectionRefByID[*target.TargetConnectionID]
			if !ok {
				return nil, fmt.Errorf("load connection %d ref for model %q", *target.TargetConnectionID, model.ModelID)
			}
			exportedTarget.ConnectionRef = &ref
		case modelrouting.IsModelTargetType(target.TargetType):
			if target.TargetModelID == nil || strings.TrimSpace(*target.TargetModelID) == "" {
				return nil, fmt.Errorf("load model target for model %q", model.ModelID)
			}
			exportedTarget.TargetModelID = target.TargetModelID
			exportedTarget.Weight = intPtr(modelrouting.EffectiveModelTargetWeight(target.Weight))
			exportedTarget.TargetPriority = intPtr(modelrouting.EffectiveModelTargetPriority(target.Position, target.TargetPriority))
		default:
			return nil, fmt.Errorf("load access target type %q for model %q", target.TargetType, model.ModelID)
		}
		exportedTargets = append(exportedTargets, exportedTarget)
	}
	return exportedTargets, nil
}

func buildConnectionExports(connections []connectionRow, endpointByID map[int]endpointRow, pricingTemplateByID map[int]pricingTemplateRow) ([]connectionExport, map[int]string, error) {
	exportedConnections := make([]connectionExport, 0, len(connections))
	connectionRefByID := make(map[int]string, len(connections))
	usedRefs := map[string]int{}
	for _, connection := range connections {
		endpoint, ok := endpointByID[connection.EndpointID]
		if !ok {
			return nil, nil, fmt.Errorf("load endpoint %d for connection %d", connection.EndpointID, connection.ID)
		}
		pricingTemplateName, err := buildConnectionPricingTemplateName(connection, pricingTemplateByID)
		if err != nil {
			return nil, nil, err
		}
		ref := connectionExportRef(connection, endpoint.Name, usedRefs)
		connectionRefByID[connection.ID] = ref
		exportedConnections = append(exportedConnections, connectionExport{
			Ref:                                  ref,
			APIFamily:                            connection.APIFamily,
			EndpointName:                         endpoint.Name,
			ContextWindowTokens:                  intPtrFromOptional(connection.ContextWindowTokens),
			DefaultOutputTokenReserve:            intPtr(connection.DefaultOutputTokenReserve),
			MaxContextUtilization:                float64Ptr(connection.MaxContextUtilization),
			PreferredContextUtilizationThreshold: float64PtrFromOptional(connection.PreferredContextUtilizationThreshold),
			PricingTemplateName:                  pricingTemplateName,
			IsActive:                             connection.IsActive,
			Priority:                             connection.Priority,
			Name:                                 connection.Name,
			AuthType:                             connection.AuthType,
			CustomHeaders:                        connection.CustomHeaders,
			OpenAIProbeEndpointVariant:           connection.OpenAIProbeEndpointVariant,
			QPSLimit:                             connection.QPSLimit,
			MaxInFlightNonStream:                 connection.MaxInFlightNonStream,
			MaxInFlightStream:                    connection.MaxInFlightStream,
		})
	}
	return exportedConnections, connectionRefByID, nil
}

func connectionExportRef(connection connectionRow, endpointName string, usedRefs map[string]int) string {
	base := strings.ToLower(strings.TrimSpace(connection.APIFamily + "-" + endpointName))
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = fmt.Sprintf("connection-%d", connection.ID)
	}
	usedRefs[base]++
	if usedRefs[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, usedRefs[base])
}

func buildConnectionPricingTemplateName(connection connectionRow, pricingTemplateByID map[int]pricingTemplateRow) (*string, error) {
	if connection.PricingTemplateID == nil {
		return nil, nil
	}
	template, ok := pricingTemplateByID[*connection.PricingTemplateID]
	if !ok {
		return nil, fmt.Errorf("load pricing template %d for connection %d", *connection.PricingTemplateID, connection.ID)
	}
	return &template.Name, nil
}

func buildProfileSettingsExport(userSettings *userSettingsRow, fxMappings []endpointFXMappingRow, connectionRefByID map[int]string) (profileSettingsExport, error) {
	reportCurrencyCode := "USD"
	reportCurrencySymbol := "$"
	var timezonePreference *string
	if userSettings != nil {
		reportCurrencyCode = userSettings.ReportCurrencyCode
		reportCurrencySymbol = userSettings.ReportCurrencySymbol
		timezonePreference = userSettings.TimezonePreference
	}
	exportedFXMappings, err := buildEndpointFXMappingExports(fxMappings, connectionRefByID)
	if err != nil {
		return profileSettingsExport{}, err
	}
	return profileSettingsExport{
		ReportCurrencyCode:   reportCurrencyCode,
		ReportCurrencySymbol: reportCurrencySymbol,
		TimezonePreference:   timezonePreference,
		EndpointFXMappings:   exportedFXMappings,
	}, nil
}

func buildEndpointFXMappingExports(fxMappings []endpointFXMappingRow, connectionRefByID map[int]string) ([]endpointFXMappingExport, error) {
	exportedFXMappings := make([]endpointFXMappingExport, 0, len(fxMappings))
	for _, mapping := range fxMappings {
		connectionRef, ok := connectionRefByID[mapping.ConnectionID]
		if !ok {
			return nil, fmt.Errorf("load connection %d for FX mapping %q", mapping.ConnectionID, mapping.ModelID)
		}
		exportedFXMappings = append(exportedFXMappings, endpointFXMappingExport{ModelID: mapping.ModelID, ConnectionRef: connectionRef, FXRate: mapping.FXRate})
	}
	return exportedFXMappings, nil
}

func buildHeaderBlocklistRuleExports(rules []headerBlocklistRuleRow) []headerBlocklistRuleExport {
	exportedRules := make([]headerBlocklistRuleExport, 0, len(rules))
	for _, rule := range rules {
		exportedRules = append(exportedRules, headerBlocklistRuleExport(rule))
	}
	return exportedRules
}

func buildUserAgentClientRuleExports(rules []userAgentClientRuleRow) []userAgentClientRuleExport {
	exportedRules := make([]userAgentClientRuleExport, 0, len(rules))
	for _, rule := range rules {
		exportedRules = append(exportedRules, userAgentClientRuleExport(rule))
	}
	return exportedRules
}

func listOrderedEndpoints(ctx context.Context, exec queryExecutor, profileID int) ([]endpointRow, error) {
	rows, err := exec.Query(ctx, `SELECT id, name, base_url, api_key, position FROM endpoints WHERE profile_id = $1 ORDER BY position ASC, id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query endpoints for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]endpointRow, 0)
	for rows.Next() {
		item := endpointRow{}
		if err := rows.Scan(&item.ID, &item.Name, &item.BaseURL, &item.APIKey, &item.Position); err != nil {
			return nil, fmt.Errorf("scan endpoint row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoints for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listPricingTemplates(ctx context.Context, exec queryExecutor, profileID int) ([]pricingTemplateRow, error) {
	rows, err := exec.Query(ctx, `SELECT id, name, description, pricing_unit, pricing_currency_code, COALESCE(input_price::text, '0'), COALESCE(output_price::text, '0'), COALESCE(cached_input_price::text, '0'), COALESCE(cache_creation_price::text, '0'), COALESCE(reasoning_price::text, '0'), version FROM pricing_templates WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query pricing templates for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]pricingTemplateRow, 0)
	for rows.Next() {
		var description sql.NullString
		item := pricingTemplateRow{}
		if err := rows.Scan(&item.ID, &item.Name, &description, &item.PricingUnit, &item.PricingCurrencyCode, &item.InputPrice, &item.OutputPrice, &item.CachedInputPrice, &item.CacheCreationPrice, &item.ReasoningPrice, &item.Version); err != nil {
			return nil, fmt.Errorf("scan pricing template row: %w", err)
		}
		item.Description = nullableStringValue(description)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing templates for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listStrategies(ctx context.Context, exec queryExecutor, profileID int) ([]strategyRow, error) {
	rows, err := exec.Query(ctx, `SELECT id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds FROM loadbalance_strategies WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query loadbalance strategies for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]strategyRow, 0)
	for rows.Next() {
		var statusCodes []int32
		item := strategyRow{}
		if err := rows.Scan(&item.ID, &item.Name, &item.LegacyStrategyType, &statusCodes, &item.BanMode, &item.RetryBaseDelayMS, &item.RetryBackoffMultiplier, &item.RetryJitterRatio, &item.RetryMaxDelayMS, &item.CycleRetryAttemptLimit, &item.BanCumulativeRetryAttemptThreshold, &item.BanDurationSeconds); err != nil {
			return nil, fmt.Errorf("scan loadbalance strategy row: %w", err)
		}
		item.FailureStatusCodes = intSliceFromInt32(statusCodes)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate loadbalance strategies for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listModels(ctx context.Context, exec queryExecutor, profileID int) ([]modelRow, error) {
	rows, err := exec.Query(ctx, `SELECT id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, context_window_tokens, default_output_token_reserve, max_context_utilization, preferred_context_utilization_threshold, facade_enabled, facade_selection_policy, facade_fallback_policy, context_overflow_promotion_target_id, is_enabled FROM model_configs WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query models for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]modelRow, 0)
	for rows.Next() {
		var vendorID sql.NullInt32
		var displayName sql.NullString
		var strategyID sql.NullInt32
		var contextWindowTokens sql.NullInt32
		var preferredContextUtilizationThreshold sql.NullFloat64
		var facadeSelectionPolicy sql.NullString
		var facadeFallbackPolicy sql.NullString
		var contextOverflowPromotionTargetID sql.NullString
		item := modelRow{}
		if err := rows.Scan(&item.ID, &vendorID, &item.APIFamily, &item.ModelID, &displayName, &strategyID, &contextWindowTokens, &item.DefaultOutputTokenReserve, &item.MaxContextUtilization, &preferredContextUtilizationThreshold, &item.FacadeEnabled, &facadeSelectionPolicy, &facadeFallbackPolicy, &contextOverflowPromotionTargetID, &item.IsEnabled); err != nil {
			return nil, fmt.Errorf("scan model row: %w", err)
		}
		item.VendorID = nullableInt32(vendorID)
		item.DisplayName = nullableStringValue(displayName)
		item.LoadbalanceStrategyID = nullableInt32(strategyID)
		item.ContextWindowTokens = nullableInt32(contextWindowTokens)
		item.PreferredContextUtilizationThreshold = nullableFloat64(preferredContextUtilizationThreshold)
		item.FacadeSelectionPolicy = nullableStringValue(facadeSelectionPolicy)
		item.FacadeFallbackPolicy = nullableStringValue(facadeFallbackPolicy)
		item.ContextOverflowPromotionTargetID = nullableStringValue(contextOverflowPromotionTargetID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate models for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listAccessTargetsByModelIDs(ctx context.Context, exec queryExecutor, modelIDs []int) (map[int][]accessTargetRow, error) {
	items := map[int][]accessTargetRow{}
	if len(modelIDs) == 0 {
		return items, nil
	}
	rows, err := exec.Query(ctx, `SELECT model_access_targets.source_model_config_id, model_access_targets.target_type, target_models.model_id, model_access_targets.target_connection_id, model_access_targets.position, model_access_targets.weight, model_access_targets.target_priority, model_access_targets.is_enabled FROM model_access_targets LEFT JOIN model_configs AS target_models ON target_models.id = model_access_targets.target_model_config_id WHERE model_access_targets.source_model_config_id = ANY($1) ORDER BY model_access_targets.source_model_config_id ASC, model_access_targets.position ASC, model_access_targets.id ASC`, toInt32Slice(modelIDs))
	if err != nil {
		return nil, fmt.Errorf("query access targets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var targetModelID sql.NullString
		var targetConnectionID sql.NullInt32
		var weight sql.NullInt32
		var targetPriority sql.NullInt32
		item := accessTargetRow{}
		if err := rows.Scan(&item.SourceModelConfigID, &item.TargetType, &targetModelID, &targetConnectionID, &item.Position, &weight, &targetPriority, &item.IsEnabled); err != nil {
			return nil, fmt.Errorf("scan access target row: %w", err)
		}
		item.TargetModelID = nullableStringValue(targetModelID)
		item.TargetConnectionID = nullableInt32(targetConnectionID)
		item.Weight = nullableInt32(weight)
		item.TargetPriority = nullableInt32(targetPriority)
		items[item.SourceModelConfigID] = append(items[item.SourceModelConfigID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate access targets: %w", err)
	}
	return items, nil
}

func listConnections(ctx context.Context, exec queryExecutor, profileID int) ([]connectionRow, error) {
	rows, err := exec.Query(ctx, `SELECT id, api_family, endpoint_id, context_window_tokens, default_output_token_reserve, max_context_utilization, preferred_context_utilization_threshold, pricing_template_id, is_active, priority, name, auth_type, custom_headers, openai_probe_endpoint_variant, qps_limit, max_in_flight_non_stream, max_in_flight_stream FROM connections WHERE profile_id = $1 ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]connectionRow, 0)
	for rows.Next() {
		var contextWindowTokens sql.NullInt32
		var preferredContextUtilizationThreshold sql.NullFloat64
		var pricingTemplateID sql.NullInt32
		var name sql.NullString
		var authType sql.NullString
		var customHeaders sql.NullString
		var probeVariant sql.NullString
		var qpsLimit sql.NullInt32
		var maxNonStream sql.NullInt32
		var maxStream sql.NullInt32
		item := connectionRow{}
		if err := rows.Scan(&item.ID, &item.APIFamily, &item.EndpointID, &contextWindowTokens, &item.DefaultOutputTokenReserve, &item.MaxContextUtilization, &preferredContextUtilizationThreshold, &pricingTemplateID, &item.IsActive, &item.Priority, &name, &authType, &customHeaders, &probeVariant, &qpsLimit, &maxNonStream, &maxStream); err != nil {
			return nil, fmt.Errorf("scan connection row: %w", err)
		}
		item.ContextWindowTokens = nullableInt32(contextWindowTokens)
		item.PreferredContextUtilizationThreshold = nullableFloat64(preferredContextUtilizationThreshold)
		item.PricingTemplateID = nullableInt32(pricingTemplateID)
		item.Name = nullableStringValue(name)
		item.AuthType = nullableStringValue(authType)
		item.CustomHeaders = parseCustomHeaders(customHeaders)
		item.OpenAIProbeEndpointVariant = nullableStringValue(probeVariant)
		item.QPSLimit = nullableInt32(qpsLimit)
		item.MaxInFlightNonStream = nullableInt32(maxNonStream)
		item.MaxInFlightStream = nullableInt32(maxStream)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connections for profile %d: %w", profileID, err)
	}
	return items, nil
}

func loadVendorsByIDs(ctx context.Context, exec queryExecutor, vendorIDs []int) (map[int]vendorRow, error) {
	items := map[int]vendorRow{}
	if len(vendorIDs) == 0 {
		return items, nil
	}
	rows, err := exec.Query(ctx, `SELECT id, key, name, description, icon_key, audit_enabled, audit_capture_bodies FROM vendors WHERE id = ANY($1) ORDER BY key ASC, id ASC`, toInt32Slice(vendorIDs))
	if err != nil {
		return nil, fmt.Errorf("query vendors: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var description sql.NullString
		var iconKey sql.NullString
		item := vendorRow{}
		if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &iconKey, &item.AuditEnabled, &item.AuditCaptureBodies); err != nil {
			return nil, fmt.Errorf("scan vendor row: %w", err)
		}
		item.Description = nullableStringValue(description)
		item.IconKey = nullableStringValue(iconKey)
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vendors: %w", err)
	}
	return items, nil
}

func loadUserSettings(ctx context.Context, exec queryExecutor, profileID int) (*userSettingsRow, error) {
	var timezone sql.NullString
	item := userSettingsRow{}
	err := exec.QueryRow(ctx, `SELECT report_currency_code, report_currency_symbol, timezone_preference FROM user_settings WHERE profile_id = $1 ORDER BY id ASC LIMIT 1`, profileID).Scan(&item.ReportCurrencyCode, &item.ReportCurrencySymbol, &timezone)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load user settings for profile %d: %w", profileID, err)
	}
	item.TimezonePreference = nullableStringValue(timezone)
	return &item, nil
}

func listEndpointFXMappings(ctx context.Context, exec queryExecutor, profileID int) ([]endpointFXMappingRow, error) {
	rows, err := exec.Query(ctx, `SELECT endpoint_fx_rate_settings.model_id, connections.id, endpoint_fx_rate_settings.fx_rate::text FROM endpoint_fx_rate_settings JOIN model_configs ON model_configs.profile_id = endpoint_fx_rate_settings.profile_id AND model_configs.model_id = endpoint_fx_rate_settings.model_id JOIN model_access_targets ON model_access_targets.profile_id = endpoint_fx_rate_settings.profile_id AND model_access_targets.source_model_config_id = model_configs.id AND model_access_targets.target_type = 'connection' JOIN connections ON connections.id = model_access_targets.target_connection_id AND connections.endpoint_id = endpoint_fx_rate_settings.endpoint_id WHERE endpoint_fx_rate_settings.profile_id = $1 ORDER BY endpoint_fx_rate_settings.model_id ASC, endpoint_fx_rate_settings.endpoint_id ASC, connections.id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query endpoint fx mappings for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]endpointFXMappingRow, 0)
	for rows.Next() {
		item := endpointFXMappingRow{}
		if err := rows.Scan(&item.ModelID, &item.ConnectionID, &item.FXRate); err != nil {
			return nil, fmt.Errorf("scan endpoint fx mapping row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoint fx mappings for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listProfileHeaderBlocklistRules(ctx context.Context, exec queryExecutor, profileID int) ([]headerBlocklistRuleRow, error) {
	rows, err := exec.Query(ctx, `SELECT name, match_type, pattern, enabled FROM header_blocklist_rules WHERE profile_id = $1 AND is_system = FALSE ORDER BY match_type ASC, pattern ASC, name ASC, id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query header blocklist rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]headerBlocklistRuleRow, 0)
	for rows.Next() {
		item := headerBlocklistRuleRow{}
		if err := rows.Scan(&item.Name, &item.MatchType, &item.Pattern, &item.Enabled); err != nil {
			return nil, fmt.Errorf("scan header blocklist rule row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate header blocklist rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listProfileUserAgentClientRules(ctx context.Context, exec queryExecutor, profileID int) ([]userAgentClientRuleRow, error) {
	rows, err := exec.Query(ctx, `SELECT name, pattern, enabled FROM user_agent_client_rules WHERE profile_id = $1 AND is_system = FALSE ORDER BY pattern ASC, name ASC, id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query user-agent client rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]userAgentClientRuleRow, 0)
	for rows.Next() {
		item := userAgentClientRuleRow{}
		if err := rows.Scan(&item.Name, &item.Pattern, &item.Enabled); err != nil {
			return nil, fmt.Errorf("scan user-agent client rule row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user-agent client rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listAllVendors(ctx context.Context, exec queryExecutor) ([]vendorCatalogRow, error) {
	rows, err := exec.Query(ctx, `SELECT key, name, description, icon_key, audit_enabled, audit_capture_bodies FROM vendors ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("query vendor catalog: %w", err)
	}
	defer rows.Close()

	items := make([]vendorCatalogRow, 0)
	for rows.Next() {
		var description sql.NullString
		var iconKey sql.NullString
		item := vendorCatalogRow{}
		if err := rows.Scan(&item.Key, &item.Name, &description, &iconKey, &item.AuditEnabled, &item.AuditCaptureBodies); err != nil {
			return nil, fmt.Errorf("scan vendor catalog row: %w", err)
		}
		item.Description = nullableStringValue(description)
		item.IconKey = nullableStringValue(iconKey)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vendor catalog: %w", err)
	}
	return items, nil
}

func parseCustomHeaders(value sql.NullString) map[string]string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return nil
	}
	return parsed
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func toInt32Slice(values []int) []int32 {
	converted := make([]int32, 0, len(values))
	for _, value := range values {
		converted = append(converted, int32(value))
	}
	return converted
}

func intSliceFromInt32(values []int32) []int {
	converted := make([]int, 0, len(values))
	for _, value := range values {
		converted = append(converted, int(value))
	}
	return converted
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}

func intPtrFromOptional(value *int) *int {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func float64Ptr(value float64) *float64 {
	resolved := value
	return &resolved
}

func float64PtrFromOptional(value *float64) *float64 {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	resolved := value.Float64
	return &resolved
}

func cloneBytes(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}
