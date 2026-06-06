import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { AlertCircle, CheckCircle2, RefreshCw, Save, ShieldAlert } from "lucide-react";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field";
import { Separator } from "@/components/ui/separator";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { api } from "@/lib/api";
import { cn } from "@/lib/utils";
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
import { StartupTelemetrySection } from "./startup/StartupTelemetrySection";
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
  getValidationStatusLabel,
  normalizeBootstrapValues,
  normalizeMailValues,
  normalizeTelemetryValues,
  telemetryValuesForNewOrIncompleteConfig,
  parseNullableFloat,
  parseNullableInteger,
  parseOrigins,
  summarizeApplyResult,
  summarizeBootstrapFileState,
  validateStartupValues,
  type DangerousConfirmation,
  type FieldErrors,
  type SecretInputState,
  type SettingsStartupCopy,
  type ValidationRow,
} from "./startup/startupFieldMetadata";

function StartupSectionGroup({
  children,
  className,
  testId,
}: {
  children: ReactNode;
  className?: string;
  testId: string;
}) {
  return (
    <section
      data-testid={testId}
      className={cn("space-y-6 rounded-2xl border border-border/70 bg-muted/10 p-4 md:p-5", className)}
    >
      {children}
    </section>
  );
}

interface StartupReviewPanelProps {
  activeDangerousConfirmations: DangerousConfirmation[];
  confirmedTokens: BootstrapConfigConfirmationToken[];
  controlsDisabled: boolean;
  copy: SettingsStartupCopy;
  dangerousConfirmations: DangerousConfirmation[];
  dirtySummary: string[];
  handleSave: () => void;
  handleValidate: () => Promise<void>;
  saving: boolean;
  toggleConfirmation: (token: BootstrapConfigConfirmationToken, checked: boolean) => void;
  validating: boolean;
  validationRows: ValidationRow[];
  writable: boolean;
}

function StartupReviewPanel({
  activeDangerousConfirmations,
  confirmedTokens,
  controlsDisabled,
  copy,
  dangerousConfirmations,
  dirtySummary,
  handleSave,
  handleValidate,
  saving,
  toggleConfirmation,
  validating,
  validationRows,
  writable,
}: StartupReviewPanelProps) {
  return (
    <Card data-testid="startup-review-panel" className="gap-0 overflow-hidden xl:max-h-[calc(100vh-2rem)]">
      <CardHeader className="border-b">
        <CardTitle className="flex items-center gap-2 text-sm">
          <CheckCircle2 />
          {copy.reviewAndSaveTitle}
        </CardTitle>
        <CardDescription>{copy.reviewAndSaveDescription}</CardDescription>
      </CardHeader>
      <CardContent className="flex min-h-0 flex-1 flex-col gap-4 py-[var(--density-card-pad-y)]">
        <div className="flex flex-wrap gap-2">
          {dirtySummary.length
            ? dirtySummary.map((item) => (
                <Badge key={item} variant="secondary">
                  {item}
                </Badge>
              ))
            : <Badge variant="outline">{copy.noLocalChangesDetected}</Badge>}
          {activeDangerousConfirmations.length ? <Badge variant="destructive">{copy.dangerousChangesStaged}</Badge> : null}
        </div>
        <FieldSet>
          <FieldLegend>{copy.dangerousChecklistTitle}</FieldLegend>
          <FieldDescription>{copy.dangerousChecklistDescription}</FieldDescription>
          <FieldGroup data-slot="checkbox-group">
            {dangerousConfirmations.map((confirmation) => (
              <Field key={confirmation.token} orientation="horizontal" data-disabled={!confirmation.active || controlsDisabled || undefined}>
                <Checkbox
                  id={`startup-review-${confirmation.token}`}
                  checked={confirmedTokens.includes(confirmation.token as BootstrapConfigConfirmationToken)}
                  disabled={!confirmation.active || controlsDisabled}
                  onCheckedChange={(checked) => toggleConfirmation(confirmation.token as BootstrapConfigConfirmationToken, checked === true)}
                />
                <FieldContent>
                  <FieldLabel htmlFor={`startup-review-${confirmation.token}`}>{confirmation.label}</FieldLabel>
                  <FieldDescription>
                    {confirmation.active ? copy.confirmationRequiredBeforeSave : copy.noChangeCurrentlyStaged}
                  </FieldDescription>
                </FieldContent>
              </Field>
            ))}
          </FieldGroup>
        </FieldSet>
        <Separator />
        <div className="min-h-0 overflow-auto scrollbar-thin">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{copy.status}</TableHead>
                <TableHead>{copy.field}</TableHead>
                <TableHead>{copy.message}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {validationRows.map((row, index) => (
                <TableRow key={`${row.field}-${index}`}>
                  <TableCell>
                    <Badge variant={row.status === "error" ? "destructive" : row.status === "warning" ? "secondary" : "outline"}>
                      {getValidationStatusLabel(row.status, copy)}
                    </Badge>
                  </TableCell>
                  <TableCell>{row.field}</TableCell>
                  <TableCell className="whitespace-normal">{row.message}</TableCell>
                </TableRow>
              ))}
              {!validationRows.length ? (
                <TableRow>
                  <TableCell colSpan={3} className="text-muted-foreground">
                    {copy.noValidationRunYet}
                  </TableCell>
                </TableRow>
              ) : null}
            </TableBody>
          </Table>
        </div>
      </CardContent>
      <CardFooter className="border-t justify-end gap-2">
        <Button type="button" variant="outline" disabled={controlsDisabled} onClick={() => void handleValidate()}>
          {validating ? <RefreshCw data-icon="inline-start" className="animate-spin" /> : <CheckCircle2 data-icon="inline-start" />}
          {copy.validate}
        </Button>
        <Button type="button" disabled={controlsDisabled || saving || !writable} onClick={handleSave}>
          {saving ? <RefreshCw data-icon="inline-start" className="animate-spin" /> : <Save data-icon="inline-start" />}
          {copy.saveStartupConfig}
        </Button>
      </CardFooter>
    </Card>
  );
}

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
  const [showDesktopReviewPanel, setShowDesktopReviewPanel] = useState(false);

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

  useEffect(() => {
    if (typeof window === "undefined") {
      return undefined;
    }
    const media = window.matchMedia("(min-width: 1280px)");
    const syncDesktopReviewPanel = () => setShowDesktopReviewPanel(media.matches);
    syncDesktopReviewPanel();
    media.addEventListener("change", syncDesktopReviewPanel);
    return () => media.removeEventListener("change", syncDesktopReviewPanel);
  }, []);

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

  const setFloatField = useCallback((path: string, rawValue: string) => {
    const parsed = parseNullableFloat(rawValue);
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

  const setTelemetryEnabled = useCallback((checked: boolean) => {
    if (!checked) {
      setSecretInputs((current) => ({ ...current, "telemetry.exporter.auth.authorizationHeader": "" }));
    }
    updateValues((current) => {
      if (!checked) {
        return { ...current, telemetry: { enabled: false, exporter: null, metrics: null, traces: null } };
      }
      const currentTelemetry = normalizeTelemetryValues(current.telemetry);
      const enabledTelemetry = telemetryValuesForNewOrIncompleteConfig();
      return {
        ...current,
        telemetry: {
          enabled: true,
          exporter: currentTelemetry.exporter ?? enabledTelemetry.exporter,
          metrics: currentTelemetry.metrics ?? enabledTelemetry.metrics,
          traces: currentTelemetry.traces ?? enabledTelemetry.traces,
        },
      };
    });
  }, [updateValues]);

  const setTelemetryAuthMode = useCallback((value: string) => {
    if (value !== "authorization_header") {
      setSecretInputs((current) => ({ ...current, "telemetry.exporter.auth.authorizationHeader": "" }));
    }
    setStringField("telemetry.exporter.auth.mode", value);
  }, [setStringField]);

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
    const telemetryAuthMode = values ? normalizeTelemetryValues(values.telemetry).exporter?.auth?.mode : null;
    for (const secretKey of SECRET_KEYS) {
      if (secretKey === "mail.smtp.password" && !mailEnabled) {
        continue;
      }
      if (secretKey === "telemetry.exporter.auth.authorizationHeader" && telemetryAuthMode !== "authorization_header") {
        if (bootstrapConfig?.secrets[secretKey].configured) {
          updates[secretKey] = { action: "clear" };
        }
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
  const currentApplySummary = summarizeBootstrapFileState(bootstrapConfig, copy);
  const fieldEffect = (field: string) => <FieldEffectBadge capability={bootstrapConfig.apply_capabilities[field]} copy={copy} />;
  const sectionEffect = (fields: string[]) => <SectionEffectBadge capabilities={bootstrapConfig.apply_capabilities} copy={copy} fields={fields} />;

  return (
    <div className="flex flex-col gap-6">
      <Alert>
        <ShieldAlert />
        <AlertTitle>{copy.startupBootstrapConfigTitle}</AlertTitle>
        <AlertDescription>{copy.startupBootstrapConfigDescription}</AlertDescription>
      </Alert>
      {bootstrapConfig.apply_result || currentApplySummary.status === "warning" ? (
        <Alert variant={currentApplySummary.status === "error" ? "destructive" : "default"}>
          {bootstrapConfig.apply_result ? <RefreshCw /> : <AlertCircle />}
          <AlertTitle>{currentApplySummary.badge}</AlertTitle>
          <AlertDescription>{currentApplySummary.message}</AlertDescription>
        </Alert>
      ) : null}
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_22rem] xl:items-start">
        <div className="grid gap-6">
          <StartupFileStatusCard bootstrapConfig={bootstrapConfig} copy={copy} currentApplySummary={currentApplySummary} />
          <div className="grid gap-6 xl:grid-cols-2 xl:items-start">
            <StartupSectionGroup testId="startup-group-core">
              <StartupServerSection copy={copy} controlsDisabled={controlsDisabled} corsOriginsText={corsOriginsText} fieldErrors={fieldErrors} fieldEffect={fieldEffect} sectionEffect={sectionEffect} setCorsOriginsText={setCorsOriginsText} setServerField={setServerField} values={values} />
              <StartupDatabaseSection bootstrapConfig={bootstrapConfig} clearSecretInput={clearSecretInput} controlsDisabled={controlsDisabled} copy={copy} fieldErrors={fieldErrors} handleSecretInputChange={handleSecretInputChange} sectionEffect={sectionEffect} secretInputs={secretInputs} setNumberField={setNumberField} values={values} />
            </StartupSectionGroup>
            <StartupSectionGroup testId="startup-group-runtime">
              <StartupRuntimeSection controlsDisabled={controlsDisabled} copy={copy} fieldErrors={fieldErrors} sectionEffect={sectionEffect} setNumberField={setNumberField} setStringField={setStringField} values={values} />
              <StartupTelemetrySection bootstrapConfig={bootstrapConfig} clearSecretInput={clearSecretInput} controlsDisabled={controlsDisabled} copy={copy} fieldErrors={fieldErrors} handleSecretInputChange={handleSecretInputChange} sectionEffect={sectionEffect} secretInputs={secretInputs} setBooleanField={setBooleanField} setFloatField={setFloatField} setStringField={setStringField} setTelemetryAuthMode={setTelemetryAuthMode} setTelemetryEnabled={setTelemetryEnabled} values={values} />
            </StartupSectionGroup>
            <StartupSectionGroup testId="startup-group-secrets" className="xl:col-span-2">
              <StartupMailSecretsSection activeDangerousConfirmations={activeDangerousConfirmations} bootstrapConfig={bootstrapConfig} clearSecretInput={clearSecretInput} confirmedTokens={confirmedTokens} controlsDisabled={controlsDisabled} copy={copy} dangerDialogOpen={dangerDialogOpen} dangerousConfirmations={dangerousConfirmations} dirtySummary={dirtySummary} fieldErrors={fieldErrors} fieldEffect={fieldEffect} handleDangerDialogOpenChange={handleDangerDialogOpenChange} handleSave={handleSave} handleSecretInputChange={handleSecretInputChange} handleValidate={handleValidate} mailEnabled={mailEnabled} mailValues={mailValues} performSave={performSave} saving={saving} sectionEffect={sectionEffect} secretInputs={secretInputs} setBooleanField={setBooleanField} setMailEnabled={setMailEnabled} setMailStringField={setMailStringField} setNumberField={setNumberField} setSMTPNumberField={setSMTPNumberField} setSMTPStringField={setSMTPStringField} setStringField={setStringField} smtpControlsDisabled={smtpControlsDisabled} smtpValues={smtpValues} toggleConfirmation={toggleConfirmation} validating={validating} validationRows={validationRows} values={values} showReviewPanel={!showDesktopReviewPanel} />
            </StartupSectionGroup>
          </div>
        </div>
        {showDesktopReviewPanel ? (
          <aside className="hidden xl:sticky xl:top-4 xl:block xl:h-fit">
            <StartupReviewPanel
              activeDangerousConfirmations={activeDangerousConfirmations}
              confirmedTokens={confirmedTokens}
              controlsDisabled={controlsDisabled}
              copy={copy}
              dangerousConfirmations={dangerousConfirmations}
              dirtySummary={dirtySummary}
              handleSave={handleSave}
              handleValidate={handleValidate}
              saving={saving}
              toggleConfirmation={toggleConfirmation}
              validating={validating}
              validationRows={validationRows}
              writable={bootstrapConfig.writable}
            />
          </aside>
        ) : null}
      </div>
    </div>
  );
}
