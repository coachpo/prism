package configbundle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	managementloadbalance "github.com/coachpo/prism/backend/internal/httpapi/management/loadbalance"
	"github.com/coachpo/prism/backend/internal/vendordomain"
	"github.com/jackc/pgx/v5"
)

const (
	canonicalBundleVersion     = 1
	canonicalProfileBundleKind = "profile_config"
	canonicalVendorCatalogKind = "vendor_catalog"
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

var validProxySelectionStrategies = map[string]struct{}{
	"ordered_fallback": {},
	"weighted_static":  {},
	"priority_static":  {},
}

func importedConnectionCount(models []modelExport) int {
	total := 0
	for _, model := range models {
		total += len(model.Connections)
	}
	return total
}

func validateProfileBundleEnvelope(data profileImportRequest) error {
	if data.Version != canonicalBundleVersion {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported profile config bundle version '%d'; expected %d", data.Version, canonicalBundleVersion)}
	}
	if data.BundleKind != canonicalProfileBundleKind {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported profile config bundle kind '%s'; expected '%s'", data.BundleKind, canonicalProfileBundleKind)}
	}
	return nil
}

func validateVendorCatalogBundleEnvelope(data vendorCatalogImportRequest) error {
	if data.Version != canonicalBundleVersion {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported vendor catalog bundle version '%d'; expected %d", data.Version, canonicalBundleVersion)}
	}
	if data.BundleKind != canonicalVendorCatalogKind {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported vendor catalog bundle kind '%s'; expected '%s'", data.BundleKind, canonicalVendorCatalogKind)}
	}
	return nil
}

func (s *Service) previewProfileImport(ctx context.Context, exec queryExecutor, data profileImportRequest) (profileImportPreviewResponse, error) {
	if err := validateProfileImportRequest(data); err != nil {
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
		Version:                  canonicalBundleVersion,
		BundleKind:               canonicalProfileBundleKind,
		ReplacementScope:         buildProfileImportReplacementScope(data),
		UntouchedScope:           buildProfileImportUntouchedScope(),
		VendorSummary:            buildProfileImportVendorSummary(vendorResolutions),
		SecretSummary:            buildProfileImportSecretSummary(data, decryptableSecretRefs),
		EndpointsImported:        len(data.Endpoints),
		PricingTemplatesImported: len(data.PricingTemplates),
		StrategiesImported:       len(data.LoadbalanceStrategies),
		ModelsImported:           len(data.Models),
		ConnectionsImported:      importedConnectionCount(data.Models),
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
		Connections:           importedConnectionCount(data.Models),
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

	_, importedPairs, connectionsImported, err := insertImportedModelsAndConnections(ctx, exec, profileID, data.Models, vendorIDsByKey, endpointIDsByName, pricingIDsByName, strategyIDsByName, currentTime)
	if err != nil {
		return profileImportResponse{}, err
	}

	if err := upsertImportedProfileSettings(ctx, exec, profileID, data.ProfileSettings, endpointIDsByName, importedPairs, currentTime); err != nil {
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
		Version:        canonicalBundleVersion,
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
	if data.SecretPayload.Kind != "encrypted" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "Config import secret payload kind must be 'encrypted'"}
	}
	if data.SecretPayload.Cipher != bundleSecretCipher {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Config import secret payload cipher must be '%s'", bundleSecretCipher)}
	}

	vendorKeys := map[string]struct{}{}
	for _, vendor := range data.VendorRefs {
		key := strings.TrimSpace(vendor.Key)
		if key == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "Vendor key must not be empty"}
		}
		if _, ok := vendorKeys[key]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate vendor key: '%s'", key)}
		}
		vendorKeys[key] = struct{}{}
	}

	endpointNames := map[string]struct{}{}
	endpointSecretRefs := map[string]struct{}{}
	for _, endpoint := range data.Endpoints {
		name := strings.TrimSpace(endpoint.Name)
		if name == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "Endpoint name must not be empty"}
		}
		if _, ok := endpointNames[name]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate endpoint name: '%s'", name)}
		}
		endpointNames[name] = struct{}{}
		if warnings := endpointdomain.ValidateBaseURL(endpointdomain.NormalizeBaseURL(endpoint.BaseURL)); len(warnings) > 0 {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Endpoint '%s' has invalid base_url: %s", name, strings.Join(warnings, "; "))}
		}
		if endpoint.Position < 0 {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Endpoint '%s' has invalid position '%d'", name, endpoint.Position)}
		}

		if endpoint.APIKeySecretRef != nil {
			secretRef := strings.TrimSpace(*endpoint.APIKeySecretRef)
			if secretRef == "" {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Endpoint '%s' has invalid api_key_secret_ref", name)}
			}
			if _, ok := endpointSecretRefs[secretRef]; ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate endpoint api_key_secret_ref: '%s'", secretRef)}
			}
			endpointSecretRefs[secretRef] = struct{}{}
		}
	}

	secretRefs := map[string]struct{}{}
	for _, entry := range data.SecretPayload.Entries {
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

	pricingTemplateNames := map[string]struct{}{}
	for _, template := range data.PricingTemplates {
		name := strings.TrimSpace(template.Name)
		if name == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "Pricing template name must not be empty"}
		}
		if _, ok := pricingTemplateNames[name]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate pricing template name: '%s'", name)}
		}
		pricingTemplateNames[name] = struct{}{}
	}

	strategyNames := map[string]struct{}{}
	if _, err := canonicalizeImportedStrategies(data.LoadbalanceStrategies); err != nil {
		return err
	}
	for _, strategy := range data.LoadbalanceStrategies {
		name := strings.TrimSpace(strategy.Name)
		if name == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "Loadbalance strategy name must not be empty"}
		}
		if _, ok := strategyNames[name]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate loadbalance strategy name: '%s'", name)}
		}
		strategyNames[name] = struct{}{}
	}

	nativeModelFamilies := map[string]string{}
	seenModelIDs := map[string]struct{}{}
	importedConnectionPairs := map[string]struct{}{}
	for _, model := range data.Models {
		modelID := strings.TrimSpace(model.ModelID)
		if modelID == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "Model id must not be empty"}
		}
		if _, ok := seenModelIDs[modelID]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate model_id: '%s'", modelID)}
		}
		seenModelIDs[modelID] = struct{}{}

		apiFamily := strings.ToLower(strings.TrimSpace(model.APIFamily))
		if _, ok := validImportAPIFamilies[apiFamily]; !ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unknown api family: '%s'", model.APIFamily)}
		}
		if model.VendorKey != nil {
			vendorKey := strings.TrimSpace(*model.VendorKey)
			if _, ok := vendorKeys[vendorKey]; !ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unknown vendor key: '%s'", vendorKey)}
			}
		}

		modelType := strings.ToLower(strings.TrimSpace(model.ModelType))
		if modelType != "native" && modelType != "proxy" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Unsupported model_type '%s' for model '%s'", model.ModelType, modelID)}
		}

		if modelType == "native" {
			if model.ProxySelectionStrategy != nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Native model '%s' must not include proxy_selection_strategy", modelID)}
			}
			if len(model.ProxyTargets) > 0 {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Native model '%s' must not have proxy_targets", modelID)}
			}
			strategyName := trimmedOptionalString(model.LoadbalanceStrategyName)
			if strategyName == nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Native model '%s' must include loadbalance_strategy_name", modelID)}
			}
			if _, ok := strategyNames[*strategyName]; !ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Native model '%s' references unknown loadbalance strategy '%s'", modelID, *strategyName)}
			}
			nativeModelFamilies[modelID] = apiFamily
		} else {
			selector := normalizedImportedProxySelectionStrategy(model.ProxySelectionStrategy)
			if selector == nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' must include proxy_selection_strategy", modelID)}
			}
			if !isValidImportedProxySelectionStrategy(*selector) {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' has unknown proxy_selection_strategy '%s'", modelID, *selector)}
			}
			if trimmedOptionalString(model.LoadbalanceStrategyName) != nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' must not include loadbalance_strategy_name", modelID)}
			}
			if len(model.Connections) > 0 {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' must not have connections", modelID)}
			}
			if len(model.ProxyTargets) == 0 {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' must include proxy_targets", modelID)}
			}
			for index, target := range model.ProxyTargets {
				targetModelID := strings.TrimSpace(target.TargetModelID)
				if targetModelID == "" {
					return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' has proxy target with empty target_model_id", modelID)}
				}
				if target.Position != index {
					return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' must use contiguous proxy_targets positions starting at 0", modelID)}
				}
				if !target.weightSet || target.weightNull {
					return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' target '%s' must include weight", modelID, targetModelID)}
				}
				if target.Weight < 1 {
					return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' target '%s' has invalid weight '%d'", modelID, targetModelID, target.Weight)}
				}
				if !target.targetPrioritySet || target.targetPriorityNull {
					return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' target '%s' must include target_priority", modelID, targetModelID)}
				}
				if target.TargetPriority < 0 {
					return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' target '%s' has invalid target_priority '%d'", modelID, targetModelID, target.TargetPriority)}
				}
			}
			seenTargets := map[string]struct{}{}
			for _, target := range model.ProxyTargets {
				targetModelID := strings.TrimSpace(target.TargetModelID)
				if _, ok := seenTargets[targetModelID]; ok {
					return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Proxy model '%s' has duplicate proxy target '%s'", modelID, targetModelID)}
				}
				seenTargets[targetModelID] = struct{}{}
			}
		}

		for _, connection := range model.Connections {
			if connection.QPSLimit != nil && *connection.QPSLimit < 1 {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection for model '%s' has invalid qps_limit '%d'", modelID, *connection.QPSLimit)}
			}
			if connection.MaxInFlightNonStream != nil && *connection.MaxInFlightNonStream < 1 {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection for model '%s' has invalid max_in_flight_non_stream '%d'", modelID, *connection.MaxInFlightNonStream)}
			}

			if connection.MaxInFlightStream != nil && *connection.MaxInFlightStream < 1 {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection for model '%s' has invalid max_in_flight_stream '%d'", modelID, *connection.MaxInFlightStream)}
			}
			if err := validateConnectionAuthType(connection.AuthType); err != nil {
				return err
			}
			if _, err := normalizeOpenAIProbeEndpointVariant(apiFamily, connection.OpenAIProbeEndpointVariant); err != nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection for model '%s' %s", modelID, err.Error())}
			}
			endpointName, err := resolveImportedEndpointName(connection.EndpointName, endpointNames)
			if err != nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection for model '%s' %s", modelID, err.Error())}
			}
			if _, err := resolveImportedPricingTemplateName(connection.PricingTemplateName, pricingTemplateNames); err != nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Connection for model '%s' %s", modelID, err.Error())}
			}
			importedConnectionPairs[connectionPairKey(modelID, endpointName)] = struct{}{}
		}
	}

	for _, model := range data.Models {
		if strings.ToLower(strings.TrimSpace(model.ModelType)) != "proxy" {
			continue
		}
		apiFamily := strings.ToLower(strings.TrimSpace(model.APIFamily))
		modelID := strings.TrimSpace(model.ModelID)
		for _, target := range model.ProxyTargets {
			targetModelID := strings.TrimSpace(target.TargetModelID)
			targetFamily, ok := nativeModelFamilies[targetModelID]
			if !ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' references unknown proxy target '%s'", modelID, targetModelID)}
			}
			if targetFamily != apiFamily {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Model '%s' cannot target cross-api-family model '%s'", modelID, targetModelID)}
			}
		}
	}

	if data.ProfileSettings != nil {
		seenFXMappings := map[string]struct{}{}

		for _, mapping := range data.ProfileSettings.EndpointFXMappings {
			endpointName, err := resolveImportedEndpointName(mapping.EndpointName, endpointNames)
			if err != nil {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("FX mapping %s", err.Error())}
			}
			key := connectionPairKey(mapping.ModelID, endpointName)
			if _, ok := seenFXMappings[key]; ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate FX mapping in import for model_id='%s', endpoint_name='%s'", mapping.ModelID, endpointName)}
			}
			seenFXMappings[key] = struct{}{}
			if _, ok := importedConnectionPairs[key]; !ok {
				return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("FX mapping must reference an imported model/endpoint connection pair: model_id='%s', endpoint_name='%s'", mapping.ModelID, endpointName)}
			}
		}
	}

	seenBlocklistRules := map[string]struct{}{}
	for _, rule := range data.HeaderBlocklistRules {
		key := fmt.Sprintf("%s\x00%s", rule.MatchType, rule.Pattern)
		if _, ok := seenBlocklistRules[key]; ok {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Duplicate header blocklist rule in import for match_type='%s', pattern='%s'", rule.MatchType, rule.Pattern)}
		}
		seenBlocklistRules[key] = struct{}{}
	}
	seenUserAgentRules := map[string]struct{}{}
	for _, rule := range data.UserAgentClientRules {
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

		canonical, err := managementloadbalance.CanonicalizeImportedStrategyDocument(managementloadbalance.ImportedStrategyDocument{Name: strategy.Name, StrategyType: strategy.StrategyType, LegacyStrategyType: strategy.LegacyStrategyType, AutoRecovery: strategy.AutoRecovery, RoutingPolicy: strategy.RoutingPolicy})
		if err != nil {
			return nil, &domainError{StatusCode: http.StatusBadRequest, Detail: err.Error()}
		}
		items = append(items, importedStrategyPayload{Name: canonical.Name, StrategyType: canonical.StrategyType, LegacyStrategyType: canonical.LegacyStrategyType, AutoRecoveryJSON: canonical.AutoRecoveryJSON, RoutingPolicyJSON: canonical.RoutingPolicyJSON})
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
	_, err := exec.Exec(ctx, `LOCK TABLE endpoint_fx_rate_settings, connections, endpoints, loadbalance_strategies, model_configs, model_proxy_targets, pricing_templates, vendors, user_settings, header_blocklist_rules, user_agent_client_rules IN SHARE ROW EXCLUSIVE MODE`)
	if err != nil {
		return fmt.Errorf("lock config bundle import tables: %w", err)
	}
	return nil
}

func clearProfileImportState(ctx context.Context, exec queryExecutor, profileID int) error {
	queries := []string{
		`DELETE FROM model_proxy_targets WHERE source_model_config_id IN (SELECT id FROM model_configs WHERE profile_id = $1) OR target_model_config_id IN (SELECT id FROM model_configs WHERE profile_id = $1)`,
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
		var pricingTemplateID int
		if err := exec.QueryRow(ctx, `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12) RETURNING id`, profileID, strings.TrimSpace(template.Name), nullableString(template.Description), template.PricingUnit, template.PricingCurrencyCode, template.InputPrice, template.OutputPrice, template.CachedInputPrice, template.CacheCreationPrice, template.ReasoningPrice, template.Version, currentTime).Scan(&pricingTemplateID); err != nil {
			return nil, 0, fmt.Errorf("insert imported pricing template %q: %w", template.Name, err)
		}
		pricingIDsByName[strings.TrimSpace(template.Name)] = pricingTemplateID
	}
	return pricingIDsByName, len(templates), nil
}

func insertImportedStrategies(ctx context.Context, exec queryExecutor, profileID int, strategies []importedStrategyPayload, currentTime time.Time) (map[string]int, int, error) {
	strategyIDsByName := map[string]int{}
	for _, strategy := range strategies {
		var strategyID int
		if err := exec.QueryRow(ctx, `INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at) VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $7) RETURNING id`, profileID, strategy.Name, strategy.StrategyType, nullableString(strategy.LegacyStrategyType), nullableJSONString(strategy.AutoRecoveryJSON), nullableJSONString(strategy.RoutingPolicyJSON), currentTime).Scan(&strategyID); err != nil {
			return nil, 0, fmt.Errorf("insert imported loadbalance strategy %q: %w", strategy.Name, err)
		}

		strategyIDsByName[strategy.Name] = strategyID
	}
	return strategyIDsByName, len(strategies), nil
}

func insertImportedModelsAndConnections(ctx context.Context, exec queryExecutor, profileID int, models []modelExport, vendorIDsByKey map[string]int, endpointIDsByName map[string]int, pricingIDsByName map[string]int, strategyIDsByName map[string]int, currentTime time.Time) (map[string]int, map[string]struct{}, int, error) {
	modelIDsByModelID := map[string]int{}
	importedPairs := map[string]struct{}{}
	proxyTargetSpecs := make([]struct {
		SourceModelID string
		Targets       []proxyTargetExport
	}, 0)
	connectionsImported := 0

	for _, model := range models {
		modelID := strings.TrimSpace(model.ModelID)
		apiFamily := strings.ToLower(strings.TrimSpace(model.APIFamily))
		modelType := strings.ToLower(strings.TrimSpace(model.ModelType))
		var vendorID any
		if model.VendorKey != nil {
			vendorID = vendorIDsByKey[strings.TrimSpace(*model.VendorKey)]
		}
		var strategyID any
		var proxySelectionStrategy any
		if modelType == "native" {
			strategyID = strategyIDsByName[*trimmedOptionalString(model.LoadbalanceStrategyName)]
		} else {
			proxySelectionStrategy = *normalizedImportedProxySelectionStrategy(model.ProxySelectionStrategy)
		}
		var modelConfigID int
		if err := exec.QueryRow(ctx, `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, model_type, proxy_selection_strategy, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10) RETURNING id`, profileID, vendorID, apiFamily, modelID, nullableString(trimmedOptionalString(model.DisplayName)), modelType, proxySelectionStrategy, strategyID, model.IsEnabled, currentTime).Scan(&modelConfigID); err != nil {
			return nil, nil, 0, fmt.Errorf("insert imported model %q: %w", modelID, err)
		}
		modelIDsByModelID[modelID] = modelConfigID
		if modelType == "proxy" {
			proxyTargetSpecs = append(proxyTargetSpecs, struct {
				SourceModelID string
				Targets       []proxyTargetExport
			}{SourceModelID: modelID, Targets: model.ProxyTargets})
			continue
		}

		sortedConnections := make([]connectionExport, len(model.Connections))
		copy(sortedConnections, model.Connections)
		sort.SliceStable(sortedConnections, func(left int, right int) bool {
			return sortedConnections[left].Priority < sortedConnections[right].Priority
		})
		for priority, connection := range sortedConnections {
			endpointName := strings.TrimSpace(connection.EndpointName)
			pricingTemplateName := trimmedOptionalString(connection.PricingTemplateName)
			probeVariant, err := normalizeOpenAIProbeEndpointVariant(apiFamily, connection.OpenAIProbeEndpointVariant)
			if err != nil {
				return nil, nil, 0, err
			}
			var customHeaders any
			if len(connection.CustomHeaders) > 0 {
				rawHeaders, marshalErr := json.Marshal(connection.CustomHeaders)
				if marshalErr != nil {
					return nil, nil, 0, fmt.Errorf("marshal custom headers for model %q: %w", modelID, marshalErr)
				}
				customHeaders = string(rawHeaders)
			}
			if _, err := exec.Exec(ctx, `INSERT INTO connections (profile_id, model_config_id, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14, $15, $16, $17, $17)`, profileID, modelConfigID, endpointIDsByName[endpointName], nullableInt(pricingIDsByName, pricingTemplateName), connection.QPSLimit, connection.MaxInFlightNonStream, connection.MaxInFlightStream, nullableString(probeVariant), connection.IsActive, priority, nullableString(trimmedOptionalString(connection.Name)), nullableString(normalizedOptionalAuthType(connection.AuthType)), customHeaders, "unknown", nil, nil, currentTime); err != nil {
				return nil, nil, 0, fmt.Errorf("insert imported connection for model %q: %w", modelID, err)
			}
			importedPairs[connectionPairKey(modelID, endpointName)] = struct{}{}
			connectionsImported++
		}
	}

	for _, proxyTargets := range proxyTargetSpecs {
		sourceModelID := modelIDsByModelID[proxyTargets.SourceModelID]
		for _, target := range proxyTargets.Targets {
			targetModelID := strings.TrimSpace(target.TargetModelID)
			if _, err := exec.Exec(ctx, `INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position, weight, target_priority) VALUES ($1, $2, $3, $4, $5)`, sourceModelID, modelIDsByModelID[targetModelID], target.Position, target.Weight, target.TargetPriority); err != nil {
				return nil, nil, 0, fmt.Errorf("insert proxy target for model %q: %w", proxyTargets.SourceModelID, err)
			}
		}
	}

	return modelIDsByModelID, importedPairs, connectionsImported, nil
}

func upsertImportedProfileSettings(ctx context.Context, exec queryExecutor, profileID int, profileSettings *profileSettingsExport, endpointIDsByName map[string]int, importedPairs map[string]struct{}, currentTime time.Time) error {
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
		endpointName := strings.TrimSpace(mapping.EndpointName)
		if _, ok := importedPairs[connectionPairKey(mapping.ModelID, endpointName)]; !ok {
			continue
		}
		if _, err := exec.Exec(ctx, `INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)`, profileID, mapping.ModelID, endpointIDsByName[endpointName], mapping.FXRate, currentTime); err != nil {
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

func normalizedImportedProxySelectionStrategy(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	if normalized == "" {
		return nil
	}
	return &normalized
}

func isValidImportedProxySelectionStrategy(value string) bool {
	_, ok := validProxySelectionStrategies[value]
	return ok
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

func connectionPairKey(modelID string, endpointName string) string {
	return fmt.Sprintf("%s\x00%s", modelID, endpointName)
}
