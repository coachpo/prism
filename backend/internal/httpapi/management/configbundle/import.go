package configbundle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/contextcapability"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	"github.com/coachpo/prism/backend/internal/vendordomain"
	"github.com/jackc/pgx/v5"
)

const (
	canonicalProfileBundleVersion                    = 3
	canonicalVendorCatalogVersion                    = 1
	canonicalProfileBundleKind                       = "profile_config"
	canonicalVendorCatalogKind                       = "vendor_catalog"
	facadeSelectionPolicyWeightedEligibleContext     = "weighted_eligible_context"
	facadeFallbackPolicyRedistributeIneligibleWeight = "redistribute_ineligible_weight"
	facadeEnabledRequiresOpenAIDetail                = "facade_enabled requires api_family 'openai'"
	nestedFacadesNotSupportedDetail                  = "nested facades are not supported"
)

var validImportAPIFamilies = map[string]struct{}{
	"openai":    {},
	"anthropic": {},
	"gemini":    {},
}

var validConnectionAuthTypes = map[string]struct{}{
	"openai":    {},
	"anthropic": {},
	"gemini":    {},
}

var importedPricingTemplateDecimalPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

func importedConnectionCount(connections []connectionExport) int {
	return len(connections)
}

func validateProfileBundleEnvelope(data profileImportRequest) error {
	if data.Version != canonicalProfileBundleVersion {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported profile config bundle version '%d'; expected %d", data.Version, canonicalProfileBundleVersion)}
	}
	if data.BundleKind != canonicalProfileBundleKind {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported profile config bundle kind '%s'; expected '%s'", data.BundleKind, canonicalProfileBundleKind)}
	}
	return nil
}

func validateVendorCatalogBundleEnvelope(data vendorCatalogImportRequest) error {
	if data.Version != canonicalVendorCatalogVersion {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported vendor catalog bundle version '%d'; expected %d", data.Version, canonicalVendorCatalogVersion)}
	}
	if data.BundleKind != canonicalVendorCatalogKind {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported vendor catalog bundle kind '%s'; expected '%s'", data.BundleKind, canonicalVendorCatalogKind)}
	}
	return nil
}

func (s *Service) previewProfileImport(ctx context.Context, exec queryExecutor, profileID int, data profileImportRequest) (profileImportPreviewResponse, error) {
	if err := validateProfileImportRequest(data); err != nil {
		return profileImportPreviewResponse{}, err
	}
	if err := validateExistingProfileConnectionOwnership(ctx, exec, profileID); err != nil {
		return profileImportPreviewResponse{}, err
	}

	_, vendorResolutions, blockingErrors, err := previewImportVendors(ctx, exec, data.VendorRefs)
	if err != nil {
		return profileImportPreviewResponse{}, err
	}
	decryptedSecrets, err := s.decryptImportSecretPayload(data.SecretPayload)
	if err != nil {
		return profileImportPreviewResponse{}, err
	}

	warnings := make([]string, 0, len(vendorResolutions))
	for _, resolution := range vendorResolutions {
		if resolution.Warning != nil {
			warnings = append(warnings, *resolution.Warning)
		}
	}

	return buildProfilePreviewResponse(data, vendorResolutions, sortedSecretRefs(decryptedSecrets), blockingErrors, warnings), nil
}

func buildProfilePreviewResponse(data profileImportRequest, vendorResolutions []profileImportVendorResolution, decryptableSecretRefs []string, blockingErrors []string, warnings []string) profileImportPreviewResponse {
	return profileImportPreviewResponse{
		Ready:                    len(blockingErrors) == 0,
		Version:                  canonicalProfileBundleVersion,
		BundleKind:               canonicalProfileBundleKind,
		ReplacementScope:         buildProfileImportReplacementScope(data),
		UntouchedScope:           buildProfileImportUntouchedScope(),
		VendorSummary:            buildProfileImportVendorSummary(vendorResolutions),
		SecretSummary:            buildProfileImportSecretSummary(data, decryptableSecretRefs),
		EndpointsImported:        len(data.Endpoints),
		PricingTemplatesImported: len(data.PricingTemplates),
		StrategiesImported:       len(data.LoadbalanceStrategies),
		ModelsImported:           len(data.Models),
		ConnectionsImported:      importedConnectionCount(data.Connections),
		VendorResolutions:        vendorResolutions,
		SecretKeyID:              data.SecretPayload.KeyID,
		DecryptableSecretRefs:    decryptableSecretRefs,
		BlockingErrors:           blockingErrors,
		Warnings:                 warnings,
	}
}

func buildProfileImportReplacementScope(data profileImportRequest) profileImportReplacementScope {
	return profileImportReplacementScope{
		Target:                "selected_profile",
		Endpoints:             len(data.Endpoints),
		PricingTemplates:      len(data.PricingTemplates),
		LoadbalanceStrategies: len(data.LoadbalanceStrategies),
		Models:                len(data.Models),
		Connections:           importedConnectionCount(data.Connections),
		HeaderBlocklistRules:  len(data.HeaderBlocklistRules),
		UserAgentClientRules:  len(data.UserAgentClientRules),
		ProfileSettings:       data.ProfileSettings != nil,
	}
}

func buildProfileImportUntouchedScope() profileImportUntouchedScope {
	return profileImportUntouchedScope{OtherProfiles: true, ExistingGlobalVendorMetadata: true, RequestLogs: true}
}

func buildProfileImportVendorSummary(vendorResolutions []profileImportVendorResolution) profileImportVendorSummary {
	createCount := 0
	warningCount := 0
	for _, resolution := range vendorResolutions {
		if resolution.Resolution == "create" {
			createCount++
		}
		if resolution.Warning != nil {
			warningCount++
		}
	}
	return profileImportVendorSummary{CreateCount: createCount, ReuseCount: len(vendorResolutions) - createCount, WarningCount: warningCount}
}

func buildProfileImportSecretSummary(data profileImportRequest, decryptableSecretRefs []string) profileImportSecretSummary {
	endpointSecretRefs := 0
	for _, endpoint := range data.Endpoints {
		if trimmedOptionalString(endpoint.APIKeySecretRef) != nil {
			endpointSecretRefs++
		}
	}
	return profileImportSecretSummary{EndpointSecretRefs: endpointSecretRefs, SecretPayloadEntries: len(data.SecretPayload.Entries), DecryptableSecretRefs: len(decryptableSecretRefs)}
}

func (s *Service) executeProfileImport(ctx context.Context, exec queryExecutor, profileID int, data profileImportRequest) (profileImportResponse, error) {
	if err := validateProfileImportRequest(data); err != nil {
		return profileImportResponse{}, err
	}
	if err := lockProfileRow(ctx, exec, profileID); err != nil {
		return profileImportResponse{}, err
	}
	if err := lockImportTargetTables(ctx, exec); err != nil {
		return profileImportResponse{}, err
	}
	if err := validateExistingProfileConnectionOwnership(ctx, exec, profileID); err != nil {
		return profileImportResponse{}, err
	}

	existingVendorsByKey, _, blockingErrors, err := previewImportVendors(ctx, exec, data.VendorRefs)
	if err != nil {
		return profileImportResponse{}, err
	}
	if len(blockingErrors) > 0 {
		return profileImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: blockingErrors[0]}
	}

	decryptedSecrets, err := s.decryptImportSecretPayload(data.SecretPayload)
	if err != nil {
		return profileImportResponse{}, err
	}
	if err := clearProfileImportState(ctx, exec, profileID); err != nil {
		return profileImportResponse{}, err
	}

	currentTime := s.nowUTC()
	vendorIDsByKey, err := ensureImportVendors(ctx, exec, data.VendorRefs, existingVendorsByKey, currentTime)
	if err != nil {
		return profileImportResponse{}, err
	}
	endpointIDsByName, endpointsImported, err := insertImportedEndpoints(ctx, exec, profileID, data.Endpoints, decryptedSecrets, s.secretEncryptionKey, currentTime)
	if err != nil {
		return profileImportResponse{}, err
	}
	pricingIDsByName, pricingImported, err := insertImportedPricingTemplates(ctx, exec, profileID, data.PricingTemplates, currentTime)
	if err != nil {
		return profileImportResponse{}, err
	}
	strategies, err := canonicalizeImportedStrategies(data.LoadbalanceStrategies)
	if err != nil {
		return profileImportResponse{}, err
	}
	strategyIDsByName, strategiesImported, err := insertImportedStrategies(ctx, exec, profileID, strategies, currentTime)
	if err != nil {
		return profileImportResponse{}, err
	}

	connectionIDsByRef, importedPairs, connectionsImported, err := insertImportedModelsAndConnections(ctx, exec, profileID, data.Models, data.Connections, vendorIDsByKey, endpointIDsByName, pricingIDsByName, strategyIDsByName, currentTime)
	if err != nil {
		return profileImportResponse{}, err
	}

	if err := upsertImportedProfileSettings(ctx, exec, profileID, data.ProfileSettings, connectionIDsByRef, importedPairs, currentTime); err != nil {
		return profileImportResponse{}, err
	}
	if err := insertImportedHeaderBlocklistRules(ctx, exec, profileID, data.HeaderBlocklistRules, currentTime); err != nil {
		return profileImportResponse{}, err
	}
	if err := insertImportedUserAgentClientRules(ctx, exec, profileID, data.UserAgentClientRules, currentTime); err != nil {
		return profileImportResponse{}, err
	}
	if s.afterProfileImport != nil {
		tx, ok := exec.(pgx.Tx)
		if !ok {
			return profileImportResponse{}, fmt.Errorf("config bundle import requires pgx transaction")
		}
		if err := s.afterProfileImport(ctx, tx); err != nil {
			return profileImportResponse{}, err
		}
	}

	return profileImportResponse{
		EndpointsImported:        endpointsImported,
		PricingTemplatesImported: pricingImported,
		StrategiesImported:       strategiesImported,
		ModelsImported:           len(data.Models),
		ConnectionsImported:      connectionsImported,
	}, nil
}

func (s *Service) previewVendorCatalogImport(ctx context.Context, exec queryExecutor, data vendorCatalogImportRequest) (vendorCatalogImportPreviewResponse, error) {
	data = normalizeVendorCatalogImportRequest(data)
	createCount, updateCount, unchangedCount, blockingErrors, _, err := countVendorCatalogChanges(ctx, exec, data)
	if err != nil {
		return vendorCatalogImportPreviewResponse{}, err
	}
	return buildVendorCatalogPreviewResponse(createCount, updateCount, unchangedCount, blockingErrors), nil
}

func buildVendorCatalogPreviewResponse(createCount int, updateCount int, unchangedCount int, blockingErrors []string) vendorCatalogImportPreviewResponse {
	return vendorCatalogImportPreviewResponse{
		Ready:          len(blockingErrors) == 0,
		Version:        canonicalVendorCatalogVersion,
		BundleKind:     canonicalVendorCatalogKind,
		MutationScope:  buildVendorCatalogImportMutationScope(createCount, updateCount, unchangedCount),
		UntouchedScope: buildVendorCatalogImportUntouchedScope(),
		CreateCount:    createCount,
		UpdateCount:    updateCount,
		BlockingErrors: blockingErrors,
		Warnings:       []string{},
	}
}

func buildVendorCatalogImportMutationScope(createCount int, updateCount int, unchangedCount int) vendorCatalogImportMutationScope {
	return vendorCatalogImportMutationScope{Target: "global_vendor_catalog", CreateCount: createCount, UpdateCount: updateCount, UnchangedCount: unchangedCount}
}

func buildVendorCatalogImportUntouchedScope() vendorCatalogImportUntouchedScope {
	return vendorCatalogImportUntouchedScope{Profiles: true, ProfileScopedConfig: true, RequestLogs: true}
}

func (s *Service) importVendorCatalog(ctx context.Context, exec queryExecutor, data vendorCatalogImportRequest) (vendorCatalogImportResponse, error) {
	data = normalizeVendorCatalogImportRequest(data)
	_, _, _, blockingErrors, existingByKey, err := countVendorCatalogChanges(ctx, exec, data)
	if err != nil {
		return vendorCatalogImportResponse{}, err
	}
	if len(blockingErrors) > 0 {
		return vendorCatalogImportResponse{}, &domainError{StatusCode: http.StatusBadRequest, Detail: blockingErrors[0]}
	}

	createdCount := 0
	updatedCount := 0
	currentTime := s.nowUTC()
	for _, vendor := range data.Vendors {
		existing := existingByKey[vendor.Key]
		if existing == nil {
			if _, err := exec.Exec(ctx, `INSERT INTO vendors (key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`, vendor.Key, vendor.Name, nullableString(vendor.Description), nullableString(vendor.IconKey), vendor.AuditEnabled, vendor.AuditCaptureBodies, currentTime); err != nil {
				return vendorCatalogImportResponse{}, fmt.Errorf("insert vendor %q: %w", vendor.Key, err)
			}
			createdCount++
			continue
		}
		if existing.Name != vendor.Name || !sameOptionalString(existing.Description, vendor.Description) || !sameOptionalString(existing.IconKey, vendor.IconKey) || existing.AuditEnabled != vendor.AuditEnabled || existing.AuditCaptureBodies != vendor.AuditCaptureBodies {
			if _, err := exec.Exec(ctx, `UPDATE vendors SET name = $2, description = $3, icon_key = $4, audit_enabled = $5, audit_capture_bodies = $6, updated_at = $7 WHERE id = $1`, existing.ID, vendor.Name, nullableString(vendor.Description), nullableString(vendor.IconKey), vendor.AuditEnabled, vendor.AuditCaptureBodies, currentTime); err != nil {
				return vendorCatalogImportResponse{}, fmt.Errorf("update vendor %q: %w", vendor.Key, err)
			}
			updatedCount++
		}
	}

	return vendorCatalogImportResponse{CreatedCount: createdCount, UpdatedCount: updatedCount}, nil
}

func validateProfileImportRequest(data profileImportRequest) error {
	if err := validateProfileBundleEnvelope(data); err != nil {
		return err
	}
	if err := validateImportedSecretPayloadEnvelope(data.SecretPayload); err != nil {
		return err
	}

	vendorKeys, err := validateImportedVendorRefs(data.VendorRefs)
	if err != nil {
		return err
	}
	endpointNames, endpointSecretRefs, err := validateImportedEndpoints(data.Endpoints)
	if err != nil {
		return err
	}
	if err := validateImportedSecretRefs(data.SecretPayload, endpointSecretRefs); err != nil {
		return err
	}
	pricingTemplateNames, err := validateImportedPricingTemplates(data.PricingTemplates)
	if err != nil {
		return err
	}
	strategyNames, err := validateImportedLoadbalanceStrategies(data.LoadbalanceStrategies)
	if err != nil {
		return err
	}

	modelRefs := profileImportModelValidationRefs{
		vendorKeys:           vendorKeys,
		endpointNames:        endpointNames,
		pricingTemplateNames: pricingTemplateNames,
		strategyNames:        strategyNames,
	}
	connectionRefs, err := validateImportedConnections(data.Connections, modelRefs)
	if err != nil {
		return err
	}
	importedModels, importedConnectionPairs, err := validateImportedModels(data.Models, modelRefs, connectionRefs)
	if err != nil {
		return err
	}
	if err := validateImportedConnectionPreferredContextThresholds(importedModels, data.Connections); err != nil {
		return err
	}
	if err := validateImportedAccessTargetReferences(importedModels); err != nil {
		return err
	}
	if err := validateImportedProfileSettings(data.ProfileSettings, connectionRefs, importedConnectionPairs); err != nil {
		return err
	}
	if err := validateImportedHeaderBlocklistRules(data.HeaderBlocklistRules); err != nil {
		return err
	}
	return validateImportedUserAgentClientRules(data.UserAgentClientRules)
}

func validateImportedSecretPayloadEnvelope(secretPayload secretPayloadExport) error {
	if secretPayload.Kind != "encrypted" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "Config import secret payload kind must be 'encrypted'"}
	}
	if secretPayload.Cipher != bundleSecretCipher {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Config import secret payload cipher must be '%s'", bundleSecretCipher)}
	}
	return nil
}

func validateImportedVendorRefs(vendorRefs []vendorRefExport) (map[string]struct{}, error) {
	vendorKeys := map[string]struct{}{}
	for _, vendor := range vendorRefs {
		key := strings.TrimSpace(vendor.Key)
		if key == "" {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "Vendor key must not be empty"}
		}
		if _, ok := vendorKeys[key]; ok {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate vendor key: '%s'", key)}
		}
		vendorKeys[key] = struct{}{}
	}
	return vendorKeys, nil
}

func validateImportedEndpoints(endpoints []endpointExport) (map[string]struct{}, map[string]struct{}, error) {
	endpointNames := map[string]struct{}{}
	endpointSecretRefs := map[string]struct{}{}
	for _, endpoint := range endpoints {
		name := strings.TrimSpace(endpoint.Name)
		if name == "" {
			return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "Endpoint name must not be empty"}
		}
		if _, ok := endpointNames[name]; ok {
			return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate endpoint name: '%s'", name)}
		}
		endpointNames[name] = struct{}{}
		if warnings := endpointdomain.ValidateBaseURL(endpointdomain.NormalizeBaseURL(endpoint.BaseURL)); len(warnings) > 0 {
			return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Endpoint '%s' has invalid base_url: %s", name, strings.Join(warnings, "; "))}
		}
		if endpoint.Position < 0 {
			return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Endpoint '%s' has invalid position '%d'", name, endpoint.Position)}
		}

		if endpoint.APIKeySecretRef != nil {
			secretRef := strings.TrimSpace(*endpoint.APIKeySecretRef)
			if secretRef == "" {
				return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Endpoint '%s' has invalid api_key_secret_ref", name)}
			}
			if _, ok := endpointSecretRefs[secretRef]; ok {
				return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate endpoint api_key_secret_ref: '%s'", secretRef)}
			}
			endpointSecretRefs[secretRef] = struct{}{}
		}
	}
	return endpointNames, endpointSecretRefs, nil
}

func validateImportedSecretRefs(secretPayload secretPayloadExport, endpointSecretRefs map[string]struct{}) error {
	secretRefs := map[string]struct{}{}
	for _, entry := range secretPayload.Entries {
		ref := strings.TrimSpace(entry.Ref)
		if _, ok := secretRefs[ref]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate secret ref: '%s'", ref)}
		}
		secretRefs[ref] = struct{}{}
	}
	missingSecretRefs := make([]string, 0)
	for ref := range endpointSecretRefs {
		if _, ok := secretRefs[ref]; !ok {
			missingSecretRefs = append(missingSecretRefs, ref)
		}
	}
	if len(missingSecretRefs) > 0 {
		sort.Strings(missingSecretRefs)
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Import is missing encrypted secret payload entries for refs: %s", strings.Join(missingSecretRefs, ", "))}
	}
	return nil
}

func validateImportedPricingTemplates(templates []pricingTemplateExport) (map[string]struct{}, error) {
	pricingTemplateNames := map[string]struct{}{}
	for _, template := range templates {
		name := strings.TrimSpace(template.Name)
		if name == "" {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "Pricing template name must not be empty"}
		}
		if _, ok := pricingTemplateNames[name]; ok {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate pricing template name: '%s'", name)}
		}
		if _, err := normalizeImportedPricingTemplatePrices(template); err != nil {
			return nil, err
		}
		pricingTemplateNames[name] = struct{}{}
	}
	return pricingTemplateNames, nil
}

func validateImportedLoadbalanceStrategies(strategies []loadbalanceStrategyExport) (map[string]struct{}, error) {
	strategyNames := map[string]struct{}{}
	if _, err := canonicalizeImportedStrategies(strategies); err != nil {
		return nil, err
	}
	for _, strategy := range strategies {
		name := strings.TrimSpace(strategy.Name)
		if name == "" {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "Loadbalance strategy name must not be empty"}
		}
		if _, ok := strategyNames[name]; ok {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate loadbalance strategy name: '%s'", name)}
		}
		strategyNames[name] = struct{}{}
	}
	return strategyNames, nil
}

type profileImportModelValidationRefs struct {
	vendorKeys           map[string]struct{}
	endpointNames        map[string]struct{}
	pricingTemplateNames map[string]struct{}
	strategyNames        map[string]struct{}
}

func validateImportedModels(models []modelExport, refs profileImportModelValidationRefs, connectionRefs map[string]importedConnectionValidationRef) ([]importedModelPayload, map[string]struct{}, error) {
	importedModels := normalizeImportedModels(models)
	seenModelIDs := map[string]struct{}{}
	connectionOwners := map[string]connectionOwnerRef{}
	importedConnectionPairs := map[string]struct{}{}
	for _, model := range importedModels {
		modelID := model.ModelID
		if modelID == "" {
			return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "Model id must not be empty"}
		}
		if _, ok := seenModelIDs[modelID]; ok {
			return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate model_id: '%s'", modelID)}
		}
		seenModelIDs[modelID] = struct{}{}
		if _, ok := validImportAPIFamilies[model.APIFamily]; !ok {
			return nil, nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unknown api family: '%s'", model.APIFamily)}
		}
		if err := validateImportedModelVendorRef(model, refs.vendorKeys); err != nil {
			return nil, nil, err
		}
		if err := validateImportedModelStrategy(model, refs.strategyNames); err != nil {
			return nil, nil, err
		}
		if err := validateImportedModelContextCapabilities(model); err != nil {
			return nil, nil, err
		}
		if err := validateImportedFacadeConfiguration(model); err != nil {
			return nil, nil, err
		}
		owner := connectionOwnerRef{ModelID: modelID, DisplayName: model.DisplayName}
		if err := validateImportedAccessTargets(modelID, model.APIFamily, owner, model.AccessTargets, connectionRefs, connectionOwners, importedConnectionPairs); err != nil {
			return nil, nil, err
		}
	}
	if err := validateImportedConnectionOwners(connectionRefs, connectionOwners); err != nil {
		return nil, nil, err
	}
	return importedModels, importedConnectionPairs, nil
}

func normalizeImportedModels(models []modelExport) []importedModelPayload {
	items := make([]importedModelPayload, 0, len(models))
	for _, model := range models {
		items = append(items, importedModelPayload{
			VendorKey:                            trimmedOptionalString(model.VendorKey),
			APIFamily:                            strings.ToLower(strings.TrimSpace(model.APIFamily)),
			ModelID:                              strings.TrimSpace(model.ModelID),
			DisplayName:                          trimmedOptionalString(model.DisplayName),
			LoadbalanceStrategyName:              trimmedOptionalString(model.LoadbalanceStrategyName),
			ContextWindowTokens:                  model.ContextWindowTokens,
			DefaultOutputTokenReserve:            model.DefaultOutputTokenReserve,
			MaxContextUtilization:                model.MaxContextUtilization,
			PreferredContextUtilizationThreshold: model.PreferredContextUtilizationThreshold,
			FacadeEnabled:                        model.FacadeEnabled,
			FacadeSelectionPolicy:                normalizeImportedOptionalString(model.FacadeSelectionPolicy, true),
			FacadeFallbackPolicy:                 normalizeImportedOptionalString(model.FacadeFallbackPolicy, true),
			IsEnabled:                            model.IsEnabled,
			AccessTargets:                        normalizeImportedAccessTargets(model.AccessTargets),
		})
	}
	return items
}

func normalizeImportedAccessTargets(targets []accessTargetExport) []importedAccessTargetPayload {
	items := make([]importedAccessTargetPayload, 0, len(targets))
	for _, target := range targets {
		resolvedWeight := 1
		if target.Weight != nil {
			resolvedWeight = *target.Weight
		}
		resolvedTargetPriority := target.Position
		if target.TargetPriority != nil {
			resolvedTargetPriority = *target.TargetPriority
		}
		items = append(items, importedAccessTargetPayload{
			Position:               target.Position,
			IsEnabled:              target.IsEnabled,
			TargetType:             strings.ToLower(strings.TrimSpace(target.TargetType)),
			ConnectionRef:          trimmedOptionalString(target.ConnectionRef),
			TargetModelID:          trimmedOptionalString(target.TargetModelID),
			Weight:                 target.Weight,
			ResolvedWeight:         resolvedWeight,
			TargetPriority:         target.TargetPriority,
			ResolvedTargetPriority: resolvedTargetPriority,
		})
	}
	return items
}

func normalizeImportedOptionalString(value *string, lower bool) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if lower {
		trimmed = strings.ToLower(trimmed)
	}
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func validateImportedModelVendorRef(model importedModelPayload, vendorKeys map[string]struct{}) error {
	if model.VendorKey == nil {
		return nil
	}
	if _, ok := vendorKeys[*model.VendorKey]; !ok {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unknown vendor key: '%s'", *model.VendorKey)}
	}
	return nil
}

func validateImportedModelStrategy(model importedModelPayload, strategyNames map[string]struct{}) error {
	if model.LoadbalanceStrategyName == nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' must include loadbalance_strategy_name", model.ModelID)}
	}
	if _, ok := strategyNames[*model.LoadbalanceStrategyName]; !ok {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' references unknown loadbalance strategy '%s'", model.ModelID, *model.LoadbalanceStrategyName)}
	}
	return nil
}

func validateImportedModelContextCapabilities(model importedModelPayload) error {
	if _, err := contextcapability.NormalizeContextWindowTokens(model.ContextWindowTokens); err != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' context_window_tokens %s", model.ModelID, err.Error())}
	}
	if model.DefaultOutputTokenReserve != nil {
		if _, err := contextcapability.NormalizeOutputTokenReserve(model.DefaultOutputTokenReserve); err != nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' default_output_token_reserve %s", model.ModelID, err.Error())}
		}
	}
	resolvedMaxContextUtilization, err := contextcapability.NormalizeMaxContextUtilization(model.MaxContextUtilization)
	if err != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' max_context_utilization %s", model.ModelID, err.Error())}
	}
	if _, err := contextcapability.NormalizePreferredContextUtilizationThreshold(model.PreferredContextUtilizationThreshold, resolvedMaxContextUtilization); err != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' preferred_context_utilization_threshold %s", model.ModelID, err.Error())}
	}
	return nil
}

func validateImportedFacadePolicyValues(selectionPolicy *string, fallbackPolicy *string) error {
	if selectionPolicy != nil && *selectionPolicy != facadeSelectionPolicyWeightedEligibleContext {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "facade_selection_policy must be 'weighted_eligible_context'"}
	}
	if fallbackPolicy != nil && *fallbackPolicy != facadeFallbackPolicyRedistributeIneligibleWeight {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "facade_fallback_policy must be 'redistribute_ineligible_weight'"}
	}
	return nil
}

func validateImportedFacadeConfiguration(model importedModelPayload) error {
	if err := validateImportedFacadePolicyValues(model.FacadeSelectionPolicy, model.FacadeFallbackPolicy); err != nil {
		return err
	}
	if !model.FacadeEnabled {
		return nil
	}
	if model.APIFamily != "openai" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: facadeEnabledRequiresOpenAIDetail}
	}
	if model.FacadeSelectionPolicy == nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "facade_selection_policy is required when facade_enabled is true"}
	}
	if model.FacadeFallbackPolicy == nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "facade_fallback_policy is required when facade_enabled is true"}
	}
	return nil
}

func validateImportedConnectionContextCapabilities(connection connectionExport, connectionRef string) error {
	if _, err := contextcapability.NormalizeContextWindowTokens(connection.ContextWindowTokens); err != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' context_window_tokens %s", connectionRef, err.Error())}
	}
	if connection.DefaultOutputTokenReserve != nil {
		if _, err := contextcapability.NormalizeOutputTokenReserve(connection.DefaultOutputTokenReserve); err != nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' default_output_token_reserve %s", connectionRef, err.Error())}
		}
	}
	if connection.MaxContextUtilization != nil {
		if _, err := contextcapability.NormalizeMaxContextUtilization(connection.MaxContextUtilization); err != nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' max_context_utilization %s", connectionRef, err.Error())}
		}
	}
	return nil
}

type importedConnectionValidationRef struct {
	EndpointName string
	APIFamily    string
}

type connectionOwnerRef struct {
	ModelID     string
	DisplayName *string
}

func validateImportedAccessTargets(modelID string, apiFamily string, owner connectionOwnerRef, targets []importedAccessTargetPayload, connectionRefs map[string]importedConnectionValidationRef, connectionOwners map[string]connectionOwnerRef, importedConnectionPairs map[string]struct{}) error {
	seenPositions := map[int]struct{}{}
	seenTargets := map[string]struct{}{}
	for _, target := range targets {
		if target.Position < 0 {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' access target position must be greater than or equal to 0", modelID)}
		}
		if _, ok := seenPositions[target.Position]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' access_targets must contain unique position values", modelID)}
		}
		seenPositions[target.Position] = struct{}{}
		if err := validateImportedAccessTargetMetadataContract(target); err != nil {
			return err
		}

		switch target.TargetType {
		case "connection":
			if target.ConnectionRef == nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' connection access target must include connection_ref", modelID)}
			}
			if target.TargetModelID != nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' connection access target must not include target_model_id", modelID)}
			}
			connection, ok := connectionRefs[*target.ConnectionRef]
			if !ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' references unknown connection_ref '%s'", modelID, *target.ConnectionRef)}
			}
			if connection.APIFamily != apiFamily {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' cannot target cross-api-family connection_ref '%s'", modelID, *target.ConnectionRef)}
			}
			seenKey := "connection:" + *target.ConnectionRef
			if _, ok := seenTargets[seenKey]; ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' has duplicate connection_ref access target '%s'", modelID, *target.ConnectionRef)}
			}
			seenTargets[seenKey] = struct{}{}
			if previousOwner, ok := connectionOwners[*target.ConnectionRef]; ok && previousOwner.ModelID != owner.ModelID {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: duplicateConnectionRefOwnerDetail(*target.ConnectionRef, previousOwner, owner)}
			}
			connectionOwners[*target.ConnectionRef] = owner
			importedConnectionPairs[connectionPairKey(modelID, *target.ConnectionRef)] = struct{}{}
		case "model":
			if target.TargetModelID == nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' model access target must include target_model_id", modelID)}
			}
			if target.ConnectionRef != nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' model access target must not include connection_ref", modelID)}
			}
			if *target.TargetModelID == modelID {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' access target cannot target itself", modelID)}
			}
			seenKey := "model:" + *target.TargetModelID
			if _, ok := seenTargets[seenKey]; ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' has duplicate model access target '%s'", modelID, *target.TargetModelID)}
			}
			seenTargets[seenKey] = struct{}{}
		default:
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' access target_type must be 'model' or 'connection'", modelID)}
		}
	}
	for expectedPosition := 0; expectedPosition < len(targets); expectedPosition++ {
		if _, ok := seenPositions[expectedPosition]; !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' access_targets positions must be contiguous starting at 0", modelID)}
		}
	}
	return nil
}

func validateImportedAccessTargetMetadataContract(target importedAccessTargetPayload) error {
	if target.TargetType == "connection" {
		if target.Weight != nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "weight must be omitted for terminal targets"}
		}
		if target.TargetPriority != nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "target_priority must be omitted for terminal targets"}
		}
		return nil
	}
	if target.Weight != nil && *target.Weight <= 0 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "weight must be greater than 0"}
	}
	if target.TargetPriority != nil && *target.TargetPriority < 0 {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "target_priority must be greater than or equal to 0"}
	}
	return nil
}

func validateImportedConnectionOwners(connectionRefs map[string]importedConnectionValidationRef, connectionOwners map[string]connectionOwnerRef) error {
	refs := make([]string, 0, len(connectionRefs))
	for connectionRef := range connectionRefs {
		refs = append(refs, connectionRef)
	}
	sort.Strings(refs)
	for _, connectionRef := range refs {
		if _, ok := connectionOwners[connectionRef]; !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection ref '%s' must be owned by exactly one model access target", connectionRef)}
		}
	}
	return nil
}

func validateImportedConnections(connections []connectionExport, refs profileImportModelValidationRefs) (map[string]importedConnectionValidationRef, error) {
	connectionRefs := map[string]importedConnectionValidationRef{}
	for _, connection := range connections {
		connectionRef := strings.TrimSpace(connection.Ref)
		if connectionRef == "" {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: "Connection ref must not be empty"}
		}
		if _, ok := connectionRefs[connectionRef]; ok {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate connection ref: '%s'", connectionRef)}
		}
		apiFamily := strings.ToLower(strings.TrimSpace(connection.APIFamily))
		if _, ok := validImportAPIFamilies[apiFamily]; !ok {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' has unknown api family: '%s'", connectionRef, connection.APIFamily)}
		}
		if connection.QPSLimit != nil && *connection.QPSLimit < 1 {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' has invalid qps_limit '%d'", connectionRef, *connection.QPSLimit)}
		}
		if connection.MaxInFlightNonStream != nil && *connection.MaxInFlightNonStream < 1 {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' has invalid max_in_flight_non_stream '%d'", connectionRef, *connection.MaxInFlightNonStream)}
		}
		if connection.MaxInFlightStream != nil && *connection.MaxInFlightStream < 1 {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' has invalid max_in_flight_stream '%d'", connectionRef, *connection.MaxInFlightStream)}
		}
		if err := validateImportedConnectionContextCapabilities(connection, connectionRef); err != nil {
			return nil, err
		}
		if err := validateConnectionAuthType(connection.AuthType); err != nil {
			return nil, err
		}
		if _, err := normalizeOpenAIProbeEndpointVariant(apiFamily, connection.OpenAIProbeEndpointVariant); err != nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' %s", connectionRef, err.Error())}
		}
		endpointName, err := resolveImportedEndpointName(connection.EndpointName, refs.endpointNames)
		if err != nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' %s", connectionRef, err.Error())}
		}
		if _, err := resolveImportedPricingTemplateName(connection.PricingTemplateName, refs.pricingTemplateNames); err != nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' %s", connectionRef, err.Error())}
		}
		connectionRefs[connectionRef] = importedConnectionValidationRef{EndpointName: endpointName, APIFamily: apiFamily}
	}
	return connectionRefs, nil
}

func validateImportedAccessTargetReferences(models []importedModelPayload) error {
	modelsByID := make(map[string]importedModelPayload, len(models))
	for _, model := range models {
		modelsByID[model.ModelID] = model
	}
	for _, model := range models {
		for _, target := range model.AccessTargets {
			if target.TargetType != "model" || target.TargetModelID == nil {
				continue
			}
			targetModel, ok := modelsByID[*target.TargetModelID]
			if !ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' references unknown model access target '%s'", model.ModelID, *target.TargetModelID)}
			}
			if targetModel.APIFamily != model.APIFamily {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' cannot target cross-api-family model '%s'", model.ModelID, *target.TargetModelID)}
			}
			if targetModel.FacadeEnabled {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: nestedFacadesNotSupportedDetail}
			}
		}
	}
	return nil
}

func validateImportedProfileSettings(profileSettings *profileSettingsExport, connectionRefs map[string]importedConnectionValidationRef, importedConnectionPairs map[string]struct{}) error {
	if profileSettings == nil {
		return nil
	}
	seenFXMappings := map[string]struct{}{}
	for _, mapping := range profileSettings.EndpointFXMappings {
		connectionRef := strings.TrimSpace(mapping.ConnectionRef)
		if connectionRef == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "FX mapping must include connection_ref"}
		}
		if _, ok := connectionRefs[connectionRef]; !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("FX mapping references unknown connection_ref '%s'", connectionRef)}
		}
		key := connectionPairKey(mapping.ModelID, connectionRef)
		if _, ok := seenFXMappings[key]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate FX mapping in import for model_id='%s', connection_ref='%s'", mapping.ModelID, connectionRef)}
		}
		seenFXMappings[key] = struct{}{}
		if _, ok := importedConnectionPairs[key]; !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("FX mapping must reference an imported model/connection access target pair: model_id='%s', connection_ref='%s'", mapping.ModelID, connectionRef)}
		}
	}
	return nil
}

func validateImportedHeaderBlocklistRules(rules []headerBlocklistRuleExport) error {
	seenBlocklistRules := map[string]struct{}{}
	for _, rule := range rules {
		key := fmt.Sprintf("%s\x00%s", rule.MatchType, rule.Pattern)
		if _, ok := seenBlocklistRules[key]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate header blocklist rule in import for match_type='%s', pattern='%s'", rule.MatchType, rule.Pattern)}
		}
		seenBlocklistRules[key] = struct{}{}
	}
	return nil
}

func validateImportedUserAgentClientRules(rules []userAgentClientRuleExport) error {
	seenUserAgentRules := map[string]struct{}{}
	for _, rule := range rules {
		if _, ok := seenUserAgentRules[rule.Pattern]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate user-agent client rule in import for pattern='%s'", rule.Pattern)}
		}
		seenUserAgentRules[rule.Pattern] = struct{}{}
	}
	return nil
}

func normalizeVendorCatalogImportRequest(data vendorCatalogImportRequest) vendorCatalogImportRequest {
	normalized := data
	normalized.Vendors = make([]vendorCatalogRow, 0, len(data.Vendors))
	for _, vendor := range data.Vendors {
		normalized.Vendors = append(normalized.Vendors, vendorCatalogRow{
			Key:                strings.ToLower(strings.TrimSpace(vendor.Key)),
			Name:               strings.TrimSpace(vendor.Name),
			Description:        trimmedOptionalString(vendor.Description),
			IconKey:            normalizedIconKey(vendor.IconKey),
			AuditEnabled:       vendor.AuditEnabled,
			AuditCaptureBodies: vendor.AuditCaptureBodies,
		})
	}
	return normalized
}

// normalizeImportedPricingTemplatePrices accepts legacy bundle v1 gaps at ingress:
// missing, null, empty, and whitespace-only price values all become concrete "0" strings.
func normalizeImportedPricingTemplatePrices(template pricingTemplateExport) (pricingTemplateExport, error) {
	normalized := template
	var err error
	if normalized.InputPrice, err = normalizeImportedPricingTemplatePrice(template, "input_price", template.InputPrice); err != nil {
		return pricingTemplateExport{}, err
	}
	if normalized.OutputPrice, err = normalizeImportedPricingTemplatePrice(template, "output_price", template.OutputPrice); err != nil {
		return pricingTemplateExport{}, err
	}
	if normalized.CachedInputPrice, err = normalizeImportedPricingTemplatePrice(template, "cached_input_price", template.CachedInputPrice); err != nil {
		return pricingTemplateExport{}, err
	}
	if normalized.CacheCreationPrice, err = normalizeImportedPricingTemplatePrice(template, "cache_creation_price", template.CacheCreationPrice); err != nil {
		return pricingTemplateExport{}, err
	}
	if normalized.ReasoningPrice, err = normalizeImportedPricingTemplatePrice(template, "reasoning_price", template.ReasoningPrice); err != nil {
		return pricingTemplateExport{}, err
	}
	return normalized, nil
}

func normalizeImportedPricingTemplatePrice(template pricingTemplateExport, fieldName string, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "0", nil
	}
	if !importedPricingTemplateDecimalPattern.MatchString(trimmed) {
		name := strings.TrimSpace(template.Name)
		return "", &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Pricing template '%s' %s must be a non-negative decimal string", name, fieldName)}
	}
	return trimmed, nil
}

func countVendorCatalogChanges(ctx context.Context, exec queryExecutor, data vendorCatalogImportRequest) (int, int, int, []string, map[string]*vendorRow, error) {
	if err := validateVendorCatalogBundleEnvelope(data); err != nil {
		return 0, 0, 0, nil, nil, err
	}

	seenKeys := map[string]struct{}{}
	seenNames := map[string]string{}
	blockingErrors := make([]string, 0)
	keys := make([]string, 0, len(data.Vendors))
	names := make([]string, 0, len(data.Vendors))
	for _, vendor := range data.Vendors {
		if _, ok := seenKeys[vendor.Key]; ok {
			blockingErrors = append(blockingErrors, fmt.Sprintf("Vendor catalog bundle contains duplicate vendor key '%s'", vendor.Key))
		}
		seenKeys[vendor.Key] = struct{}{}
		if existingKey, ok := seenNames[vendor.Name]; ok && existingKey != vendor.Key {
			blockingErrors = append(blockingErrors, fmt.Sprintf("Vendor catalog bundle contains duplicate vendor name '%s' for keys '%s' and '%s'", vendor.Name, existingKey, vendor.Key))
		} else {
			seenNames[vendor.Name] = vendor.Key
		}
		keys = append(keys, vendor.Key)
		names = append(names, vendor.Name)
	}

	existingByKey, existingByName, err := loadVendorsByKeysOrNames(ctx, exec, keys, names)
	if err != nil {
		return 0, 0, 0, nil, nil, err
	}
	createCount := 0
	updateCount := 0
	unchangedCount := 0
	for _, vendor := range data.Vendors {
		existing := existingByKey[vendor.Key]
		existingNameVendor := existingByName[vendor.Name]
		if vendordomain.IsReadonlyVendorKey(vendor.Key) {
			canonical, ok := vendordomain.CanonicalSystemVendor(vendor.Key)
			canonicalKey := vendor.Key
			if ok {
				canonicalKey = canonical.Key
			}
			if existing == nil {
				blockingErrors = append(blockingErrors, fmt.Sprintf("Readonly system vendor '%s' cannot be created by vendor catalog import", canonicalKey))
				continue
			}
			if !ok {
				blockingErrors = append(blockingErrors, fmt.Sprintf("Readonly system vendor '%s' is missing a canonical definition", vendor.Key))
				continue
			}

			if existing.Key != canonical.Key || existing.Name != canonical.Name || !sameOptionalString(existing.Description, stringPtr(canonical.Description)) || !sameOptionalString(existing.IconKey, stringPtr(canonical.IconKey)) || vendor.Name != canonical.Name || !sameOptionalString(vendor.Description, stringPtr(canonical.Description)) || !sameOptionalString(vendor.IconKey, stringPtr(canonical.IconKey)) || existing.AuditEnabled != vendor.AuditEnabled || existing.AuditCaptureBodies != vendor.AuditCaptureBodies {
				blockingErrors = append(blockingErrors, fmt.Sprintf("Readonly system vendor '%s' cannot be overwritten by vendor catalog import", canonicalKey))
				continue
			}
			unchangedCount++
			continue
		}
		if existing == nil {
			if existingNameVendor != nil && existingNameVendor.Key != vendor.Key {
				blockingErrors = append(blockingErrors, fmt.Sprintf("Vendor catalog import would create vendor key '%s' with name '%s' that already exists on key '%s'", vendor.Key, vendor.Name, existingNameVendor.Key))
				continue
			}
			createCount++
			continue
		}
		if existingNameVendor != nil && existingNameVendor.Key != existing.Key {
			blockingErrors = append(blockingErrors, fmt.Sprintf("Vendor catalog import would update key '%s' to duplicate existing vendor name '%s' used by key '%s'", vendor.Key, vendor.Name, existingNameVendor.Key))
			continue
		}
		if existing.Name != vendor.Name || !sameOptionalString(existing.Description, vendor.Description) || !sameOptionalString(existing.IconKey, vendor.IconKey) || existing.AuditEnabled != vendor.AuditEnabled || existing.AuditCaptureBodies != vendor.AuditCaptureBodies {
			updateCount++
			continue
		}
		unchangedCount++
	}

	return createCount, updateCount, unchangedCount, blockingErrors, existingByKey, nil
}

func previewImportVendors(ctx context.Context, exec queryExecutor, vendorRefs []vendorRefExport) (map[string]*vendorRow, []profileImportVendorResolution, []string, error) {
	keys := make([]string, 0, len(vendorRefs))
	for _, vendor := range vendorRefs {
		keys = append(keys, canonicalVendorKey(vendor.Key))
	}
	existingByKey, _, err := loadVendorsByKeysOrNames(ctx, exec, keys, nil)
	if err != nil {
		return nil, nil, nil, err
	}

	proposedNames := make([]string, 0)
	for _, vendor := range vendorRefs {
		if existingByKey[canonicalVendorKey(vendor.Key)] == nil {
			proposedNames = append(proposedNames, resolvedNewVendorName(vendor))
		}
	}
	_, existingByName, err := loadVendorsByKeysOrNames(ctx, exec, nil, proposedNames)
	if err != nil {
		return nil, nil, nil, err
	}

	resolutions := make([]profileImportVendorResolution, 0, len(vendorRefs))
	blockingErrors := make([]string, 0)
	proposedNameToKey := map[string]string{}
	for _, vendor := range vendorRefs {
		canonicalKey := canonicalVendorKey(vendor.Key)
		existing := existingByKey[canonicalKey]
		if existing == nil {
			proposedName := resolvedNewVendorName(vendor)
			if duplicateKey, ok := proposedNameToKey[proposedName]; ok && duplicateKey != vendor.Key {
				blockingErrors = append(blockingErrors, fmt.Sprintf("Config import would create duplicate global vendor name '%s' for keys '%s' and '%s'", proposedName, duplicateKey, vendor.Key))
			} else {
				proposedNameToKey[proposedName] = vendor.Key
			}
			if existingByName[proposedName] != nil && existingByName[proposedName].Key != vendor.Key {
				blockingErrors = append(blockingErrors, fmt.Sprintf("Config import vendor '%s' would create global vendor name '%s' that already exists on key '%s'", vendor.Key, proposedName, existingByName[proposedName].Key))
			}
			resolutions = append(resolutions, profileImportVendorResolution{VendorKey: vendor.Key, Resolution: "create"})
			continue
		}

		warning := vendorResolutionWarning(existing, vendor)
		resolutions = append(resolutions, profileImportVendorResolution{VendorKey: vendor.Key, Resolution: "reuse", Warning: warning})
	}
	return existingByKey, resolutions, blockingErrors, nil
}

func canonicalizeImportedStrategies(strategies []loadbalanceStrategyExport) ([]importedStrategyPayload, error) {
	items := make([]importedStrategyPayload, 0, len(strategies))
	for _, strategy := range strategies {
		canonical, err := managementloadbalance.CanonicalizeImportedStrategyDocument(managementloadbalance.ImportedStrategyDocument{
			Name:                               strategy.Name,
			LegacyStrategyType:                 strategy.LegacyStrategyType,
			FailureStatusCodes:                 strategy.FailureStatusCodes,
			BanMode:                            strategy.BanMode,
			RetryBaseDelayMS:                   strategy.RetryBaseDelayMS,
			RetryBackoffMultiplier:             strategy.RetryBackoffMultiplier,
			RetryJitterRatio:                   strategy.RetryJitterRatio,
			RetryMaxDelayMS:                    strategy.RetryMaxDelayMS,
			CycleRetryAttemptLimit:             strategy.CycleRetryAttemptLimit,
			BanCumulativeRetryAttemptThreshold: strategy.BanCumulativeRetryAttemptThreshold,
			BanDurationSeconds:                 strategy.BanDurationSeconds,
		})
		if err != nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: err.Error()}
		}
		items = append(items, importedStrategyPayload{
			Name:                               canonical.Name,
			LegacyStrategyType:                 *canonical.LegacyStrategyType,
			FailureStatusCodes:                 canonical.FailureStatusCodes,
			BanMode:                            canonical.BanMode,
			RetryBaseDelayMS:                   canonical.RetryBaseDelayMS,
			RetryBackoffMultiplier:             canonical.RetryBackoffMultiplier,
			RetryJitterRatio:                   canonical.RetryJitterRatio,
			RetryMaxDelayMS:                    canonical.RetryMaxDelayMS,
			CycleRetryAttemptLimit:             canonical.CycleRetryAttemptLimit,
			BanCumulativeRetryAttemptThreshold: canonical.BanCumulativeRetryAttemptThreshold,
			BanDurationSeconds:                 canonical.BanDurationSeconds,
		})
	}
	return items, nil
}

func (s *Service) decryptImportSecretPayload(secretPayload secretPayloadExport) (secretPayloadEntryMap, error) {
	expectedKeyID, err := s.resolvedBundleSecretKeyID()
	if err != nil {
		return nil, err
	}
	if secretPayload.KeyID != expectedKeyID {
		return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Config import bundle key mismatch: bundle key_id '%s' does not match server key_id '%s'", secretPayload.KeyID, expectedKeyID)}
	}
	decrypted := secretPayloadEntryMap{}
	for _, entry := range secretPayload.Entries {
		if !strings.HasPrefix(entry.Ciphertext, encryptedSecretPrefix) {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Config import secret ref '%s' must be encrypted", entry.Ref)}
		}
		plaintext, decryptErr := s.bundleSecretDecrypter(entry.Ciphertext)
		if decryptErr != nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Config import could not decrypt secret ref '%s'", entry.Ref)}
		}
		if strings.TrimSpace(plaintext) == "" {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Config import secret ref '%s' resolved to an empty value", entry.Ref)}
		}
		decrypted[entry.Ref] = plaintext
	}
	return decrypted, nil
}

func lockProfileRow(ctx context.Context, exec queryExecutor, profileID int) error {
	if err := exec.QueryRow(ctx, `SELECT id FROM profiles WHERE id = $1 FOR UPDATE`, profileID).Scan(new(int)); err != nil {

		return fmt.Errorf("lock profile %d: %w", profileID, err)
	}
	return nil
}

func lockImportTargetTables(ctx context.Context, exec queryExecutor) error {
	_, err := exec.Exec(ctx, `LOCK TABLE endpoint_fx_rate_settings, connections, endpoints, loadbalance_strategies, model_configs, model_access_targets, pricing_templates, vendors, user_settings, header_blocklist_rules, user_agent_client_rules IN SHARE ROW EXCLUSIVE MODE`)
	if err != nil {
		return fmt.Errorf("lock config bundle import tables: %w", err)
	}
	return nil
}

func validateExistingProfileConnectionOwnership(ctx context.Context, exec queryExecutor, profileID int) error {
	rows, err := exec.Query(ctx, `WITH collision AS (
		SELECT target_connection_id
		FROM model_access_targets
		WHERE profile_id = $1 AND target_connection_id IS NOT NULL
		GROUP BY target_connection_id
		HAVING COUNT(*) > 1
		ORDER BY target_connection_id ASC
		LIMIT 1
	)
	SELECT connections.id, connections.api_family, endpoints.name, owner_models.model_id, owner_models.display_name
	FROM collision
	JOIN connections ON connections.id = collision.target_connection_id AND connections.profile_id = $1
	JOIN endpoints ON endpoints.id = connections.endpoint_id AND endpoints.profile_id = $1
	JOIN model_access_targets ON model_access_targets.profile_id = $1 AND model_access_targets.target_connection_id = collision.target_connection_id
	JOIN model_configs AS owner_models ON owner_models.id = model_access_targets.source_model_config_id AND owner_models.profile_id = $1
	ORDER BY owner_models.model_id ASC, owner_models.id ASC`, profileID)
	if err != nil {
		return fmt.Errorf("query existing profile connection ownership: %w", err)
	}
	defer rows.Close()

	connectionID := 0
	connectionRef := ""
	owners := make([]connectionOwnerRef, 0, 2)
	for rows.Next() {
		var rowConnectionID int
		var apiFamily string
		var endpointName string
		var modelID string
		var displayName sql.NullString
		if err := rows.Scan(&rowConnectionID, &apiFamily, &endpointName, &modelID, &displayName); err != nil {
			return fmt.Errorf("scan existing profile connection ownership: %w", err)
		}
		if connectionID == 0 {
			connectionID = rowConnectionID
			connectionRef = connectionExportRef(connectionRow{ID: rowConnectionID, APIFamily: apiFamily}, endpointName, map[string]int{})
		}
		owners = append(owners, connectionOwnerRef{ModelID: modelID, DisplayName: nullableStringValue(displayName)})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate existing profile connection ownership: %w", err)
	}
	if len(owners) < 2 {
		return nil
	}
	return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Target profile has existing connection ownership collision for connection_ref '%s' (connection_id %d): %s and %s", connectionRef, connectionID, formatConnectionOwnerRef(owners[0]), formatConnectionOwnerRef(owners[1]))}
}

func clearProfileImportState(ctx context.Context, exec queryExecutor, profileID int) error {
	queries := []string{
		`DELETE FROM model_access_targets WHERE profile_id = $1`,
		`DELETE FROM endpoint_fx_rate_settings WHERE profile_id = $1`,
		`DELETE FROM connections WHERE profile_id = $1`,
		`DELETE FROM endpoints WHERE profile_id = $1`,
		`DELETE FROM model_configs WHERE profile_id = $1`,
		`DELETE FROM loadbalance_strategies WHERE profile_id = $1`,
		`DELETE FROM pricing_templates WHERE profile_id = $1`,
		`DELETE FROM header_blocklist_rules WHERE profile_id = $1 AND is_system = FALSE`,
		`DELETE FROM user_agent_client_rules WHERE profile_id = $1 AND is_system = FALSE`,
	}
	for _, query := range queries {
		if _, err := exec.Exec(ctx, query, profileID); err != nil {
			return fmt.Errorf("clear profile import state for profile %d: %w", profileID, err)
		}
	}
	return nil
}

func ensureImportVendors(ctx context.Context, exec queryExecutor, vendorRefs []vendorRefExport, existingByKey map[string]*vendorRow, currentTime time.Time) (map[string]int, error) {
	vendorIDsByKey := map[string]int{}
	for _, vendor := range vendorRefs {
		canonicalKey := canonicalVendorKey(vendor.Key)
		existing := existingByKey[canonicalKey]
		if existing == nil {
			canonical, ok := vendordomain.CanonicalSystemVendor(vendor.Key)
			name := resolvedNewVendorName(vendor)
			description := trimmedOptionalString(vendor.DescriptionHint)
			iconKey := normalizedIconKey(vendor.IconKeyHint)
			key := vendor.Key
			if ok {
				key = canonical.Key
				name = canonical.Name
				description = stringPtr(canonical.Description)
				iconKey = stringPtr(canonical.IconKey)
			}
			existing = &vendorRow{
				Key:                key,
				Name:               name,
				Description:        description,
				IconKey:            iconKey,
				AuditEnabled:       false,
				AuditCaptureBodies: true,
			}
			if err := exec.QueryRow(ctx, `INSERT INTO vendors (key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, $2, $3, $4, FALSE, TRUE, $5, $5) RETURNING id`, key, name, nullableString(description), nullableString(iconKey), currentTime).Scan(&existing.ID); err != nil {
				return nil, fmt.Errorf("insert imported vendor %q: %w", vendor.Key, err)
			}
			existingByKey[canonicalKey] = existing
		}
		vendorIDsByKey[vendor.Key] = existing.ID
	}
	return vendorIDsByKey, nil
}

func insertImportedEndpoints(ctx context.Context, exec queryExecutor, profileID int, endpoints []endpointExport, decryptedSecrets secretPayloadEntryMap, secretKey string, currentTime time.Time) (map[string]int, int, error) {
	indexed := make([]endpointExport, len(endpoints))
	copy(indexed, endpoints)
	sort.SliceStable(indexed, func(left int, right int) bool { return indexed[left].Position < indexed[right].Position })
	endpointIDsByName := map[string]int{}
	for index, endpoint := range indexed {
		apiKey := ""
		if endpoint.APIKeySecretRef != nil {
			encrypted, err := endpointdomain.EncryptSecret(decryptedSecrets[*endpoint.APIKeySecretRef], secretKey, func() time.Time { return currentTime })
			if err != nil {
				return nil, 0, fmt.Errorf("encrypt imported endpoint secret for %q: %w", endpoint.Name, err)
			}

			apiKey = encrypted
		}
		var endpointID int
		if err := exec.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, profileID, strings.TrimSpace(endpoint.Name), endpointdomain.NormalizeBaseURL(endpoint.BaseURL), apiKey, index, currentTime).Scan(&endpointID); err != nil {
			return nil, 0, fmt.Errorf("insert imported endpoint %q: %w", endpoint.Name, err)
		}
		endpointIDsByName[strings.TrimSpace(endpoint.Name)] = endpointID
	}
	return endpointIDsByName, len(indexed), nil
}

func insertImportedPricingTemplates(ctx context.Context, exec queryExecutor, profileID int, templates []pricingTemplateExport, currentTime time.Time) (map[string]int, int, error) {
	pricingIDsByName := map[string]int{}
	for _, template := range templates {
		normalized, err := normalizeImportedPricingTemplatePrices(template)
		if err != nil {
			return nil, 0, err
		}
		var pricingTemplateID int
		if err := exec.QueryRow(ctx, `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12) RETURNING id`, profileID, strings.TrimSpace(normalized.Name), nullableString(normalized.Description), normalized.PricingUnit, normalized.PricingCurrencyCode, normalized.InputPrice, normalized.OutputPrice, normalized.CachedInputPrice, normalized.CacheCreationPrice, normalized.ReasoningPrice, normalized.Version, currentTime).Scan(&pricingTemplateID); err != nil {
			return nil, 0, fmt.Errorf("insert imported pricing template %q: %w", normalized.Name, err)
		}
		pricingIDsByName[strings.TrimSpace(normalized.Name)] = pricingTemplateID
	}
	return pricingIDsByName, len(templates), nil
}

func insertImportedStrategies(ctx context.Context, exec queryExecutor, profileID int, strategies []importedStrategyPayload, currentTime time.Time) (map[string]int, int, error) {
	strategyIDsByName := map[string]int{}
	for _, strategy := range strategies {
		var strategyID int
		if err := exec.QueryRow(ctx, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, $3, $4::integer[], $5, $6, $7, $8, $9, $10, $11, $12, $13, $13) RETURNING id`, profileID, strategy.Name, strategy.LegacyStrategyType, toInt32Slice(strategy.FailureStatusCodes), strategy.BanMode, strategy.RetryBaseDelayMS, strategy.RetryBackoffMultiplier, strategy.RetryJitterRatio, strategy.RetryMaxDelayMS, strategy.CycleRetryAttemptLimit, strategy.BanCumulativeRetryAttemptThreshold, strategy.BanDurationSeconds, currentTime).Scan(&strategyID); err != nil {
			return nil, 0, fmt.Errorf("insert imported loadbalance strategy %q: %w", strategy.Name, err)
		}

		strategyIDsByName[strategy.Name] = strategyID
	}
	return strategyIDsByName, len(strategies), nil
}

func buildImportedModelCapabilitySettings(models []importedModelPayload) (map[string]contextcapability.Settings, error) {
	items := make(map[string]contextcapability.Settings, len(models))
	for _, model := range models {
		settings, err := contextcapability.NormalizeModelSettings(model.ContextWindowTokens, model.DefaultOutputTokenReserve, model.MaxContextUtilization, model.PreferredContextUtilizationThreshold)
		if err != nil {
			return nil, err
		}
		items[model.ModelID] = settings
	}
	return items, nil
}

func buildImportedConnectionOwnerSettings(models []importedModelPayload, modelSettingsByModelID map[string]contextcapability.Settings) map[string]contextcapability.Settings {
	items := map[string]contextcapability.Settings{}
	for _, model := range models {
		ownerSettings, ok := modelSettingsByModelID[model.ModelID]
		if !ok {
			continue
		}
		for _, target := range model.AccessTargets {
			if target.TargetType != "connection" || target.ConnectionRef == nil {
				continue
			}
			items[*target.ConnectionRef] = ownerSettings
		}
	}
	return items
}

func validateImportedConnectionPreferredContextThresholds(models []importedModelPayload, connections []connectionExport) error {
	modelSettingsByModelID, err := buildImportedModelCapabilitySettings(models)
	if err != nil {
		return err
	}
	connectionOwnerSettings := buildImportedConnectionOwnerSettings(models, modelSettingsByModelID)
	for _, connection := range connections {
		connectionRef := strings.TrimSpace(connection.Ref)
		settings, hasOwnerSettings := connectionOwnerSettings[connectionRef]
		if !hasOwnerSettings {
			settings = contextcapability.Settings{DefaultOutputTokenReserve: contextcapability.DefaultOutputTokenReserve, MaxContextUtilization: contextcapability.DefaultMaxContextUtilization}
		}
		if _, err := contextcapability.NormalizeConnectionSettings(settings, connection.ContextWindowTokens, connection.DefaultOutputTokenReserve, connection.MaxContextUtilization, connection.PreferredContextUtilizationThreshold); err != nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection '%s' preferred_context_utilization_threshold %s", connectionRef, err.Error())}
		}
	}
	return nil
}

func insertImportedModelsAndConnections(ctx context.Context, exec queryExecutor, profileID int, models []modelExport, connections []connectionExport, vendorIDsByKey map[string]int, endpointIDsByName map[string]int, pricingIDsByName map[string]int, strategyIDsByName map[string]int, currentTime time.Time) (map[string]int, map[string]struct{}, int, error) {
	importedModels := normalizeImportedModels(models)
	modelSettingsByModelID, err := buildImportedModelCapabilitySettings(importedModels)
	if err != nil {
		return nil, nil, 0, err
	}
	connectionOwnerSettings := buildImportedConnectionOwnerSettings(importedModels, modelSettingsByModelID)
	modelIDsByModelID := map[string]int{}
	connectionIDsByRef := map[string]int{}
	importedPairs := map[string]struct{}{}

	for _, model := range importedModels {
		settings := modelSettingsByModelID[model.ModelID]
		var vendorID any
		if model.VendorKey != nil {
			vendorID = vendorIDsByKey[*model.VendorKey]
		}
		strategyID := strategyIDsByName[*model.LoadbalanceStrategyName]
		var modelConfigID int
		if err := exec.QueryRow(ctx, `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, context_window_tokens, default_output_token_reserve, max_context_utilization, preferred_context_utilization_threshold, facade_enabled, facade_selection_policy, facade_fallback_policy, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15) RETURNING id`, profileID, vendorID, model.APIFamily, model.ModelID, nullableString(model.DisplayName), strategyID, nullableOptionalInt(settings.ContextWindowTokens), settings.DefaultOutputTokenReserve, settings.MaxContextUtilization, nullableOptionalFloat64(settings.PreferredContextUtilizationThreshold), model.FacadeEnabled, nullableString(model.FacadeSelectionPolicy), nullableString(model.FacadeFallbackPolicy), model.IsEnabled, currentTime).Scan(&modelConfigID); err != nil {
			return nil, nil, 0, fmt.Errorf("insert imported model %q: %w", model.ModelID, err)
		}
		modelIDsByModelID[model.ModelID] = modelConfigID
	}

	connectionsImported, err := insertImportedConnections(ctx, exec, profileID, connections, endpointIDsByName, pricingIDsByName, connectionOwnerSettings, connectionIDsByRef, currentTime)
	if err != nil {
		return nil, nil, 0, err
	}
	if err := insertImportedAccessTargets(ctx, exec, profileID, importedModels, modelIDsByModelID, connectionIDsByRef, importedPairs, currentTime); err != nil {
		return nil, nil, 0, err
	}
	return connectionIDsByRef, importedPairs, connectionsImported, nil
}

func insertImportedConnections(ctx context.Context, exec queryExecutor, profileID int, connections []connectionExport, endpointIDsByName map[string]int, pricingIDsByName map[string]int, connectionOwnerSettings map[string]contextcapability.Settings, connectionIDsByRef map[string]int, currentTime time.Time) (int, error) {
	for _, connection := range connections {
		connectionRef := strings.TrimSpace(connection.Ref)
		apiFamily := strings.ToLower(strings.TrimSpace(connection.APIFamily))
		endpointName := strings.TrimSpace(connection.EndpointName)
		pricingTemplateName := trimmedOptionalString(connection.PricingTemplateName)
		probeVariant, err := normalizeOpenAIProbeEndpointVariant(apiFamily, connection.OpenAIProbeEndpointVariant)
		if err != nil {
			return 0, err
		}
		settings, hasOwnerSettings := connectionOwnerSettings[connectionRef]
		if !hasOwnerSettings {
			settings = contextcapability.Settings{DefaultOutputTokenReserve: contextcapability.DefaultOutputTokenReserve, MaxContextUtilization: contextcapability.DefaultMaxContextUtilization}
		}
		resolvedSettings, err := contextcapability.NormalizeConnectionSettings(settings, connection.ContextWindowTokens, connection.DefaultOutputTokenReserve, connection.MaxContextUtilization, connection.PreferredContextUtilizationThreshold)
		if err != nil {
			return 0, fmt.Errorf("normalize imported connection %q capabilities: %w", connectionRef, err)
		}
		var customHeaders any
		if len(connection.CustomHeaders) > 0 {
			rawHeaders, marshalErr := json.Marshal(connection.CustomHeaders)
			if marshalErr != nil {
				return 0, fmt.Errorf("marshal custom headers for connection %q: %w", connectionRef, marshalErr)
			}
			customHeaders = string(rawHeaders)
		}
		var connectionID int
		if err := exec.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, context_window_tokens, default_output_token_reserve, max_context_utilization, preferred_context_utilization_threshold, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17::jsonb, $18, $19, $20, $21, $21) RETURNING id`, profileID, apiFamily, endpointIDsByName[endpointName], nullableOptionalInt(resolvedSettings.ContextWindowTokens), resolvedSettings.DefaultOutputTokenReserve, resolvedSettings.MaxContextUtilization, nullableOptionalFloat64(resolvedSettings.PreferredContextUtilizationThreshold), nullableInt(pricingIDsByName, pricingTemplateName), connection.QPSLimit, connection.MaxInFlightNonStream, connection.MaxInFlightStream, nullableString(probeVariant), connection.IsActive, connection.Priority, nullableString(trimmedOptionalString(connection.Name)), nullableString(normalizedOptionalAuthType(connection.AuthType)), customHeaders, "unknown", nil, nil, currentTime).Scan(&connectionID); err != nil {
			return 0, fmt.Errorf("insert imported connection %q: %w", connectionRef, err)
		}
		connectionIDsByRef[connectionRef] = connectionID
	}
	return len(connections), nil
}

func insertImportedAccessTargets(ctx context.Context, exec queryExecutor, profileID int, models []importedModelPayload, modelIDsByModelID map[string]int, connectionIDsByRef map[string]int, importedPairs map[string]struct{}, currentTime time.Time) error {
	for _, model := range models {
		sourceModelID := modelIDsByModelID[model.ModelID]
		sortedTargets := make([]importedAccessTargetPayload, len(model.AccessTargets))
		copy(sortedTargets, model.AccessTargets)
		sort.SliceStable(sortedTargets, func(left int, right int) bool { return sortedTargets[left].Position < sortedTargets[right].Position })
		for _, target := range sortedTargets {
			switch target.TargetType {
			case "connection":
				if _, err := exec.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, profileID, sourceModelID, connectionIDsByRef[*target.ConnectionRef], target.Position, target.IsEnabled, currentTime); err != nil {
					return fmt.Errorf("insert connection access target for model %q: %w", model.ModelID, err)
				}
				importedPairs[connectionPairKey(model.ModelID, *target.ConnectionRef)] = struct{}{}
			case "model":
				if _, err := exec.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, weight, target_priority, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, $5, $6, $7, $8, $8)`, profileID, sourceModelID, modelIDsByModelID[*target.TargetModelID], target.Position, target.ResolvedWeight, target.ResolvedTargetPriority, target.IsEnabled, currentTime); err != nil {
					return fmt.Errorf("insert model access target for model %q: %w", model.ModelID, err)
				}
			}
		}
	}
	return nil
}

func upsertImportedProfileSettings(ctx context.Context, exec queryExecutor, profileID int, profileSettings *profileSettingsExport, connectionIDsByRef map[string]int, importedPairs map[string]struct{}, currentTime time.Time) error {
	reportCurrencyCode := "USD"
	reportCurrencySymbol := "$"
	var timezonePreference *string
	if profileSettings != nil {
		reportCurrencyCode = profileSettings.ReportCurrencyCode
		reportCurrencySymbol = profileSettings.ReportCurrencySymbol
		timezonePreference = profileSettings.TimezonePreference
	}
	if _, err := exec.Exec(ctx, `INSERT INTO user_settings (profile_id, report_currency_code, report_currency_symbol, timezone_preference, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5) ON CONFLICT (profile_id) DO UPDATE SET report_currency_code = EXCLUDED.report_currency_code, report_currency_symbol = EXCLUDED.report_currency_symbol, timezone_preference = EXCLUDED.timezone_preference, updated_at = EXCLUDED.updated_at`, profileID, reportCurrencyCode, reportCurrencySymbol, nullableString(timezonePreference), currentTime); err != nil {
		return fmt.Errorf("upsert imported user settings for profile %d: %w", profileID, err)
	}
	if profileSettings == nil {
		return nil
	}
	for _, mapping := range profileSettings.EndpointFXMappings {
		connectionRef := strings.TrimSpace(mapping.ConnectionRef)
		if _, ok := importedPairs[connectionPairKey(mapping.ModelID, connectionRef)]; !ok {
			continue
		}
		if _, err := exec.Exec(ctx, `INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) SELECT $1, $2, connections.endpoint_id, $4, $5, $5 FROM connections WHERE connections.id = $3`, profileID, mapping.ModelID, connectionIDsByRef[connectionRef], mapping.FXRate, currentTime); err != nil {
			return fmt.Errorf("insert imported endpoint fx mapping for model %q: %w", mapping.ModelID, err)
		}
	}
	return nil
}

func insertImportedHeaderBlocklistRules(ctx context.Context, exec queryExecutor, profileID int, rules []headerBlocklistRuleExport, currentTime time.Time) error {
	sortedRules := make([]headerBlocklistRuleExport, len(rules))
	copy(sortedRules, rules)
	sort.SliceStable(sortedRules, func(left int, right int) bool {
		if sortedRules[left].MatchType != sortedRules[right].MatchType {
			return sortedRules[left].MatchType < sortedRules[right].MatchType
		}
		if sortedRules[left].Pattern != sortedRules[right].Pattern {
			return sortedRules[left].Pattern < sortedRules[right].Pattern
		}
		return sortedRules[left].Name < sortedRules[right].Name
	})
	for _, rule := range sortedRules {
		if _, err := exec.Exec(ctx, `INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, FALSE, $6, $6)`, profileID, rule.Name, rule.MatchType, rule.Pattern, rule.Enabled, currentTime); err != nil {
			return fmt.Errorf("insert imported header blocklist rule %q: %w", rule.Name, err)
		}
	}
	return nil
}

func insertImportedUserAgentClientRules(ctx context.Context, exec queryExecutor, profileID int, rules []userAgentClientRuleExport, currentTime time.Time) error {
	sortedRules := make([]userAgentClientRuleExport, len(rules))
	copy(sortedRules, rules)
	sort.SliceStable(sortedRules, func(left int, right int) bool {
		if sortedRules[left].Pattern != sortedRules[right].Pattern {
			return sortedRules[left].Pattern < sortedRules[right].Pattern
		}
		return sortedRules[left].Name < sortedRules[right].Name
	})
	for _, rule := range sortedRules {
		if _, err := exec.Exec(ctx, `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, $3, $4, FALSE, $5, $5)`, profileID, rule.Name, rule.Pattern, rule.Enabled, currentTime); err != nil {
			return fmt.Errorf("insert imported user-agent client rule %q: %w", rule.Name, err)
		}
	}
	return nil
}

func loadVendorsByKeysOrNames(ctx context.Context, exec queryExecutor, keys []string, names []string) (map[string]*vendorRow, map[string]*vendorRow, error) {
	byKey := map[string]*vendorRow{}
	byName := map[string]*vendorRow{}
	if len(keys) == 0 && len(names) == 0 {
		return byKey, byName, nil
	}
	if keys == nil {
		keys = []string{}
	}
	if names == nil {
		names = []string{}
	}
	rows, err := exec.Query(ctx, `SELECT id, key, name, description, icon_key, audit_enabled, audit_capture_bodies FROM vendors WHERE (cardinality($1::text[]) > 0 AND key = ANY($1)) OR (cardinality($2::text[]) > 0 AND name = ANY($2)) ORDER BY id ASC`, keys, names)
	if err != nil {
		return nil, nil, fmt.Errorf("query import vendors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item := vendorRow{}
		var description sql.NullString
		var iconKey sql.NullString
		if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &iconKey, &item.AuditEnabled, &item.AuditCaptureBodies); err != nil {
			return nil, nil, fmt.Errorf("scan import vendor row: %w", err)
		}
		item.Description = nullableStringValue(description)
		item.IconKey = nullableStringValue(iconKey)
		copy := item
		byKey[item.Key] = &copy
		byName[item.Name] = &copy
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate import vendors: %w", err)
	}
	return byKey, byName, nil
}

func sortedSecretRefs(entries secretPayloadEntryMap) []string {
	refs := make([]string, 0, len(entries))
	for ref := range entries {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func canonicalVendorKey(key string) string {
	if canonical, ok := vendordomain.CanonicalSystemVendor(key); ok {
		return canonical.Key
	}
	return strings.TrimSpace(key)
}

func resolvedNewVendorName(vendor vendorRefExport) string {
	if name := strings.TrimSpace(vendor.NameHint); name != "" {
		return name
	}
	return strings.TrimSpace(vendor.Key)
}

func vendorResolutionWarning(existing *vendorRow, vendor vendorRefExport) *string {
	fields := make([]string, 0, 3)
	if name := strings.TrimSpace(vendor.NameHint); name != "" && existing.Name != name {
		fields = append(fields, "name_hint")
	}
	if description := trimmedOptionalString(vendor.DescriptionHint); description != nil && !sameOptionalString(existing.Description, description) {
		fields = append(fields, "description_hint")
	}
	if iconKey := normalizedIconKey(vendor.IconKeyHint); iconKey != nil && !sameOptionalString(normalizedIconKey(existing.IconKey), iconKey) {
		fields = append(fields, "icon_key_hint")
	}

	if len(fields) == 0 {
		return nil
	}
	warning := fmt.Sprintf("Imported vendor hints differ from existing global vendor metadata for fields: %s", strings.Join(fields, ", "))
	return &warning
}

func resolveImportedEndpointName(endpointName string, known map[string]struct{}) (string, error) {
	resolved := strings.TrimSpace(endpointName)
	if resolved == "" {
		return "", fmt.Errorf("must include endpoint_name")
	}
	if _, ok := known[resolved]; !ok {
		return "", fmt.Errorf("references unknown endpoint_name '%s'", resolved)
	}
	return resolved, nil
}

func resolveImportedPricingTemplateName(name *string, known map[string]struct{}) (*string, error) {
	resolved := trimmedOptionalString(name)
	if resolved == nil {
		return nil, nil
	}
	if _, ok := known[*resolved]; !ok {
		return nil, fmt.Errorf("references unknown pricing_template_name '%s'", *resolved)
	}
	return resolved, nil
}

func normalizeOpenAIProbeEndpointVariant(apiFamily string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	if apiFamily != "openai" {
		return nil, fmt.Errorf("must not include openai_probe_endpoint_variant outside the OpenAI API family")
	}
	switch normalized {
	case "responses_minimal", "responses_reasoning_none", "chat_completions_minimal", "chat_completions_reasoning_none":
		return stringPtr(normalized), nil
	default:
		return nil, fmt.Errorf("has invalid openai_probe_endpoint_variant")
	}
}

func validateConnectionAuthType(value *string) error {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	if _, ok := validConnectionAuthTypes[normalized]; !ok {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "auth_type must be one of 'openai', 'anthropic', or 'gemini'"}
	}
	return nil
}

func normalizedOptionalAuthType(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	if normalized == "" {
		return nil
	}
	return &normalized
}

func trimmedOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizedIconKey(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.ToLower(strings.TrimSpace(*value))
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableOptionalInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableOptionalFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringPtr(value string) *string {
	result := value
	return &result
}

func nullableJSONString(value []byte) any {
	if len(value) == 0 {
		return nil
	}

	return string(value)
}

func nullableInt(values map[string]int, key *string) any {
	if key == nil {
		return nil
	}
	return values[*key]
}

func sameOptionalString(left *string, right *string) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func duplicateConnectionRefOwnerDetail(connectionRef string, first connectionOwnerRef, second connectionOwnerRef) string {
	return fmt.Sprintf("connection_ref '%s' is owned by multiple models: %s and %s", connectionRef, formatConnectionOwnerRef(first), formatConnectionOwnerRef(second))
}

func formatConnectionOwnerRef(owner connectionOwnerRef) string {
	displayName := trimmedOptionalString(owner.DisplayName)
	if displayName == nil || *displayName == owner.ModelID {
		return fmt.Sprintf("model_id '%s'", owner.ModelID)
	}
	return fmt.Sprintf("model_id '%s' (display_name '%s')", owner.ModelID, *displayName)
}

func connectionPairKey(modelID string, endpointName string) string {
	return fmt.Sprintf("%s\x00%s", modelID, endpointName)
}
