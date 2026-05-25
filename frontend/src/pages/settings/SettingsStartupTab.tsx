import { useCallback, useEffect, useMemo, useState } from "react";
import { AlertCircle, RefreshCw, ShieldAlert } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { api } from "@/lib/api";
import type {
  BootstrapConfigConfirmationToken,
  BootstrapConfigMailSMTPValues,
  BootstrapConfigResponse,
  BootstrapConfigSecretKey,
  BootstrapConfigSecretUpdates,
  BootstrapConfigUpdateRequest,
  BootstrapConfigValues,
} from "@/lib/types";
import { StartupDatabaseSection } from "./startup/StartupDatabaseSection";
import { StartupMailSecretsSection } from "./startup/StartupMailSecretsSection";
import { StartupRuntimeSection } from "./startup/StartupRuntimeSection";
import {
  FieldEffectBadge,
  LoadingSkeleton,
  SectionEffectBadge,
  StartupFileStatusCard,
} from "./startup/StartupServerSection";
import { StartupServerSection } from "./startup/StartupServerSection";
import {
  SECRET_KEYS,
  buildApplyResultRows,
  buildPlannedRows,
  buildPreserveSecretUpdates,
  cloneValues,
  emptyDisabledMailValuesForUiState,
  smtpValuesForNewOrIncompleteMailConfig,
  emptySecretInputs,
  extractBackendRows,
  extractBootstrapResponse,
  formatOrigins,
  getChangedCapabilityFields,
  getDangerousConfirmationLabel,
  getErrorMessage,
  normalizeBootstrapValues,
  normalizeMailValues,
  parseNullableInteger,
  parseOrigins,
  summarizeApplyResult,
  validateStartupValues,
  type DangerousConfirmation,
  type FieldErrors,
  type SecretInputState,
  type ValidationRow,
} from "./startup/startupFieldMetadata";

export function SettingsStartupTab() {
  const { messages } = useLocale();
  const copy = messages.settingsStartup;
  const [bootstrapConfig, setBootstrapConfig] = useState<BootstrapConfigResponse | null>(null);
  const [values, setValues] = useState<BootstrapConfigValues | null>(null);
  const [corsOriginsText, setCorsOriginsText] = useState("");
  const [secretInputs, setSecretInputs] = useState<SecretInputState>(() => emptySecretInputs());
  const [confirmedTokens, setConfirmedTokens] = useState<BootstrapConfigConfirmationToken[]>([]);
  const [validationRows, setValidationRows] = useState<ValidationRow[]>([]);
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [loading, setLoading] = useState(true);
  const [validating, setValidating] = useState(false);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [dangerDialogOpen, setDangerDialogOpen] = useState(false);

  const hydrateConfig = useCallback((response: BootstrapConfigResponse) => {
    const normalizedResponse = { ...response, values: normalizeBootstrapValues(response.values) };
    setBootstrapConfig(normalizedResponse);
    setValues(cloneValues(normalizedResponse.values));
    setCorsOriginsText(formatOrigins(normalizedResponse.values.http.cors_allowed_origins));
    setConfirmedTokens([]);
    setValidationRows([]);
    setFieldErrors({});
  }, []);

  const loadConfig = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      hydrateConfig(await api.config.bootstrap.get());
    } catch (error) {
      setLoadError(getErrorMessage(error, copy.failedToLoad));
    } finally {
      setLoading(false);
    }
  }, [copy.failedToLoad, hydrateConfig]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  const updateValues = useCallback((updater: (current: BootstrapConfigValues) => BootstrapConfigValues) => {
    setValues((current) => (current ? updater(current) : current));
    setValidationRows([]);
  }, []);

  const setServerField = useCallback((field: "host" | "port", rawValue: string) => {
    updateValues((current) => ({
      ...current,
      server: { ...current.server, [field]: field === "port" ? parseNullableInteger(rawValue) : rawValue.trim() || null },
    }));
  }, [updateValues]);

  const setNumberField = useCallback((path: string, rawValue: string) => {
    const parsed = parseNullableInteger(rawValue);
    updateValues((current) => {
      const next = cloneValues(current);
      const segments = path.split(".");
      let target: Record<string, unknown> = next as unknown as Record<string, unknown>;
      for (let index = 0; index < segments.length - 1; index += 1) {
        target = target[segments[index]] as Record<string, unknown>;
      }
      target[segments[segments.length - 1]] = parsed;
      return next;
    });
  }, [updateValues]);

  const setStringField = useCallback((path: string, rawValue: string) => {
    updateValues((current) => {
      const next = cloneValues(current);
      const segments = path.split(".");
      let target: Record<string, unknown> = next as unknown as Record<string, unknown>;
      for (let index = 0; index < segments.length - 1; index += 1) {
        target = target[segments[index]] as Record<string, unknown>;
      }
      target[segments[segments.length - 1]] = rawValue.trim() || null;
      return next;
    });
  }, [updateValues]);

  const setBooleanField = useCallback((path: string, checked: boolean) => {
    updateValues((current) => {
      const next = cloneValues(current);
      const segments = path.split(".");
      let target: Record<string, unknown> = next as unknown as Record<string, unknown>;
      for (let index = 0; index < segments.length - 1; index += 1) {
        target = target[segments[index]] as Record<string, unknown>;
      }
      target[segments[segments.length - 1]] = checked;
      return next;
    });
  }, [updateValues]);

  const setMailEnabled = useCallback((checked: boolean) => {
    if (!checked) {
      setSecretInputs((current) => ({ ...current, "mail.smtp.password": "" }));
    }
    updateValues((current) => {
      if (!checked) {
        return { ...current, mail: emptyDisabledMailValuesForUiState() };
      }
      const currentMail = normalizeMailValues(current.mail);
      return { ...current, mail: { ...currentMail, enabled: true, smtp: currentMail.smtp ?? smtpValuesForNewOrIncompleteMailConfig() } };
    });
  }, [updateValues]);

  const setMailStringField = useCallback((field: "from" | "reply_to", rawValue: string) => {
    updateValues((current) => {
      const currentMail = normalizeMailValues(current.mail);
      return { ...current, mail: { ...currentMail, [field]: rawValue.trim() || null } };
    });
  }, [updateValues]);

  const setSMTPStringField = useCallback((field: keyof BootstrapConfigMailSMTPValues, rawValue: string) => {
    updateValues((current) => {
      const currentMail = normalizeMailValues(current.mail);
      const currentSMTP = currentMail.smtp ?? smtpValuesForNewOrIncompleteMailConfig();
      return { ...current, mail: { ...currentMail, enabled: true, smtp: { ...currentSMTP, [field]: rawValue.trim() || null } } };
    });
  }, [updateValues]);

  const setSMTPNumberField = useCallback((field: keyof BootstrapConfigMailSMTPValues, rawValue: string) => {
    const parsed = parseNullableInteger(rawValue);
    updateValues((current) => {
      const currentMail = normalizeMailValues(current.mail);
      const currentSMTP = currentMail.smtp ?? smtpValuesForNewOrIncompleteMailConfig();
      return { ...current, mail: { ...currentMail, enabled: true, smtp: { ...currentSMTP, [field]: parsed } } };
    });
  }, [updateValues]);

  const handleSecretInputChange = useCallback((secretKey: BootstrapConfigSecretKey, value: string) => {
    if (secretKey === "runtime.secretEncryptionKey") {
      return;
    }
    setSecretInputs((current) => ({ ...current, [secretKey]: value }));
    setValidationRows([]);
  }, []);

  const clearSecretInput = useCallback((secretKey: BootstrapConfigSecretKey) => {
    setSecretInputs((current) => ({ ...current, [secretKey]: "" }));
  }, []);

  const secretUpdates = useMemo<BootstrapConfigSecretUpdates>(() => {
    const updates = buildPreserveSecretUpdates();
    const mailEnabled = values ? normalizeMailValues(values.mail).enabled : false;
    for (const secretKey of SECRET_KEYS) {
      if (secretKey === "mail.smtp.password" && !mailEnabled) {
        continue;
      }
      const replacement = secretInputs[secretKey].trim();
      const masked = bootstrapConfig?.secrets[secretKey].masked ?? "";
      if (secretKey !== "runtime.secretEncryptionKey" && replacement && replacement !== masked) {
        updates[secretKey] = { action: "replace", value: replacement };
      }
    }
    return updates;
  }, [bootstrapConfig, secretInputs, values]);

  const changedCapabilityFields = useMemo(() => {
    if (!bootstrapConfig || !values) {
      return [] as string[];
    }
    return getChangedCapabilityFields(bootstrapConfig, values, corsOriginsText, secretUpdates);
  }, [bootstrapConfig, corsOriginsText, secretUpdates, values]);

  const changedCapabilityFieldSet = useMemo(() => new Set(changedCapabilityFields), [changedCapabilityFields]);

  const dangerousConfirmations = useMemo<DangerousConfirmation[]>(() => {
    if (!bootstrapConfig) {
      return [];
    }
    return Object.entries(bootstrapConfig.apply_capabilities)
      .filter(([, capability]) => capability.mode === "restart_required" && Boolean(capability.confirmation_token))
      .map(([field, capability]) => ({
        token: capability.confirmation_token as BootstrapConfigConfirmationToken,
        label: getDangerousConfirmationLabel(copy, capability.confirmation_token ?? "", field),
        active: changedCapabilityFieldSet.has(field),
      }));
  }, [bootstrapConfig, changedCapabilityFieldSet, copy]);

  const activeDangerousConfirmations = useMemo(() => dangerousConfirmations.filter((confirmation) => confirmation.active), [dangerousConfirmations]);
  const missingDangerousConfirmations = useMemo(
    () => activeDangerousConfirmations.filter((confirmation) => !confirmedTokens.includes(confirmation.token as BootstrapConfigConfirmationToken)),
    [activeDangerousConfirmations, confirmedTokens],
  );

  const buildRequest = useCallback((confirmations: BootstrapConfigConfirmationToken[]): BootstrapConfigUpdateRequest | null => {
    if (!bootstrapConfig || !values) {
      return null;
    }
    const nextValues = normalizeBootstrapValues(values);
    nextValues.http.cors_allowed_origins = parseOrigins(corsOriginsText);
    return {
      expected_revision: bootstrapConfig.file_revision,
      expected_etag: bootstrapConfig.document_etag,
      values: nextValues,
      secret_updates: secretUpdates,
      confirmations,
    };
  }, [bootstrapConfig, corsOriginsText, secretUpdates, values]);

  const runFrontendValidation = useCallback((requireConfirmations: boolean) => {
    const { errors, rows } = validateStartupValues({ bootstrapConfig, copy, corsOriginsText, secretUpdates, values });
    if (requireConfirmations && missingDangerousConfirmations.length > 0) {
      rows.push({ field: "confirmations", message: copy.completeDangerousChecklist, status: "error" });
    }
    setFieldErrors(errors);
    setValidationRows(rows.length ? rows : [{ field: "frontend", message: copy.clientChecksPassed, status: "success" }]);
    return rows.length === 0;
  }, [bootstrapConfig, copy, corsOriginsText, missingDangerousConfirmations.length, secretUpdates, values]);

  const handleValidate = useCallback(async () => {
    if (!runFrontendValidation(false)) {
      toast.error(copy.fixClientErrorsBeforeBackendValidation);
      return;
    }
    const request = buildRequest(confirmedTokens);
    if (!request) {
      return;
    }
    setValidating(true);
    try {
      const response = await api.config.bootstrap.validate(request);
      setValidationRows(buildPlannedRows(response.planned_changes, copy));
      toast.success(copy.bootstrapConfigValidated);
    } catch (error) {
      const rows = extractBackendRows(error, copy);
      setValidationRows(rows.length ? rows : [{ field: "backend", message: getErrorMessage(error, copy.validationUnavailable), status: "error" }]);
      toast.error(copy.failedToValidate);
    } finally {
      setValidating(false);
    }
  }, [buildRequest, confirmedTokens, copy, runFrontendValidation]);

  const performSave = useCallback(async () => {
    const request = buildRequest(confirmedTokens);
    if (!request) {
      return;
    }
    setSaving(true);
    try {
      await api.config.bootstrap.validate(request);
      const response = await api.config.bootstrap.update(request);
      const summary = summarizeApplyResult(response.apply_result, copy);
      hydrateConfig(response);
      setSecretInputs(emptySecretInputs());
      setValidationRows(buildApplyResultRows(response.apply_result, copy));
      if (summary.status === "error") {
        toast.error(summary.toast);
      } else {
        toast.success(summary.toast);
      }
    } catch (error) {
      setSecretInputs(emptySecretInputs());
      const response = extractBootstrapResponse(error);
      if (response) {
        const summary = summarizeApplyResult(response.apply_result, copy);
        hydrateConfig(response);
        setValidationRows(buildApplyResultRows(response.apply_result, copy));
        toast.error(summary.toast);
      } else {
        const rows = extractBackendRows(error, copy);
        setValidationRows(rows.length ? rows : [{ field: "save", message: getErrorMessage(error, copy.failedToSave), status: "error" }]);
        toast.error(copy.failedToSave);
      }
    } finally {
      setSaving(false);
      setDangerDialogOpen(false);
    }
  }, [buildRequest, confirmedTokens, copy, hydrateConfig]);

  const handleSave = useCallback(() => {
    if (!runFrontendValidation(true)) {
      toast.error(copy.completeValidationBeforeSaving);
      return;
    }
    if (activeDangerousConfirmations.length > 0) {
      setDangerDialogOpen(true);
      return;
    }
    void performSave();
  }, [activeDangerousConfirmations.length, copy.completeValidationBeforeSaving, performSave, runFrontendValidation]);

  const dirtySummary = useMemo(() => {
    if (!bootstrapConfig) {
      return [] as string[];
    }
    const items: string[] = [];
    const hotCount = changedCapabilityFields.filter((field) => bootstrapConfig.apply_capabilities[field]?.mode === "hot_apply").length;
    const restartCount = changedCapabilityFields.filter((field) => bootstrapConfig.apply_capabilities[field]?.mode === "restart_required").length;
    if (hotCount > 0 && restartCount > 0) items.push(copy.mixedChangesStaged(hotCount, restartCount));
    else if (hotCount > 0) items.push(copy.hotApplyChangesStaged(hotCount));
    else if (restartCount > 0) items.push(copy.restartChangesStaged(restartCount));
    const replacements = SECRET_KEYS.filter((secretKey) => secretUpdates[secretKey].action === "replace");
    if (replacements.length > 0) {
      items.push(copy.secretReplacementCount(replacements.length));
    }
    return items;
  }, [bootstrapConfig, changedCapabilityFields, copy, secretUpdates]);

  const toggleConfirmation = useCallback((token: BootstrapConfigConfirmationToken, checked: boolean) => {
    setConfirmedTokens((current) => {
      if (checked) {
        return current.includes(token) ? current : [...current, token];
      }
      return current.filter((item) => item !== token);
    });
  }, []);

  const handleDangerDialogOpenChange = useCallback((open: boolean) => {
    setDangerDialogOpen(open);
    if (!open) {
      setSecretInputs(emptySecretInputs());
    }
  }, []);

  if (loading) {
    return <LoadingSkeleton />;
  }

  if (loadError || !bootstrapConfig || !values) {
    return (
      <Alert variant="destructive">
        <AlertCircle />
        <AlertTitle>{copy.loadFailedTitle}</AlertTitle>
        <AlertDescription className="gap-3">
          <p>{loadError ?? copy.loadFailedDescription}</p>
          <Button type="button" variant="outline" size="sm" onClick={() => void loadConfig()}>
            <RefreshCw data-icon="inline-start" />
            {copy.retry}
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  const controlsDisabled = saving || validating || !bootstrapConfig.writable;
  const mailValues = normalizeMailValues(values.mail);
  const smtpValues = mailValues.smtp ?? smtpValuesForNewOrIncompleteMailConfig();
  const mailEnabled = Boolean(mailValues.enabled);
  const smtpControlsDisabled = controlsDisabled || !mailEnabled;
  const currentApplySummary = summarizeApplyResult(bootstrapConfig.apply_result, copy);
  const fieldEffect = (field: string) => <FieldEffectBadge capability={bootstrapConfig.apply_capabilities[field]} copy={copy} />;
  const sectionEffect = (fields: string[]) => <SectionEffectBadge capabilities={bootstrapConfig.apply_capabilities} copy={copy} fields={fields} />;

  return (
    <div className="flex flex-col gap-6">
      <Alert>
        <ShieldAlert />
        <AlertTitle>{copy.startupBootstrapConfigTitle}</AlertTitle>
        <AlertDescription>{copy.startupBootstrapConfigDescription}</AlertDescription>
      </Alert>
      {bootstrapConfig.apply_result ? (
        <Alert variant={currentApplySummary.status === "error" ? "destructive" : "default"}>
          <RefreshCw />
          <AlertTitle>{currentApplySummary.badge}</AlertTitle>
          <AlertDescription>{currentApplySummary.message}</AlertDescription>
        </Alert>
      ) : null}
      <StartupFileStatusCard bootstrapConfig={bootstrapConfig} copy={copy} currentApplySummary={currentApplySummary} />
      <div className="grid gap-6 xl:grid-cols-2">
        <StartupServerSection copy={copy} controlsDisabled={controlsDisabled} corsOriginsText={corsOriginsText} fieldErrors={fieldErrors} fieldEffect={fieldEffect} sectionEffect={sectionEffect} setCorsOriginsText={setCorsOriginsText} setServerField={setServerField} values={values} />
        <StartupDatabaseSection bootstrapConfig={bootstrapConfig} clearSecretInput={clearSecretInput} controlsDisabled={controlsDisabled} copy={copy} fieldErrors={fieldErrors} fieldEffect={fieldEffect} handleSecretInputChange={handleSecretInputChange} sectionEffect={sectionEffect} secretInputs={secretInputs} setNumberField={setNumberField} values={values} />
        <StartupRuntimeSection controlsDisabled={controlsDisabled} copy={copy} fieldErrors={fieldErrors} fieldEffect={fieldEffect} sectionEffect={sectionEffect} setNumberField={setNumberField} setStringField={setStringField} values={values} />
        <StartupMailSecretsSection activeDangerousConfirmations={activeDangerousConfirmations} bootstrapConfig={bootstrapConfig} clearSecretInput={clearSecretInput} confirmedTokens={confirmedTokens} controlsDisabled={controlsDisabled} copy={copy} dangerDialogOpen={dangerDialogOpen} dangerousConfirmations={dangerousConfirmations} dirtySummary={dirtySummary} fieldErrors={fieldErrors} fieldEffect={fieldEffect} handleDangerDialogOpenChange={handleDangerDialogOpenChange} handleSave={handleSave} handleSecretInputChange={handleSecretInputChange} handleValidate={handleValidate} mailEnabled={mailEnabled} mailValues={mailValues} performSave={performSave} saving={saving} sectionEffect={sectionEffect} secretInputs={secretInputs} setBooleanField={setBooleanField} setMailEnabled={setMailEnabled} setMailStringField={setMailStringField} setNumberField={setNumberField} setSMTPNumberField={setSMTPNumberField} setSMTPStringField={setSMTPStringField} setStringField={setStringField} smtpControlsDisabled={smtpControlsDisabled} smtpValues={smtpValues} toggleConfirmation={toggleConfirmation} validating={validating} validationRows={validationRows} values={values} />
      </div>
    </div>
  );
}
