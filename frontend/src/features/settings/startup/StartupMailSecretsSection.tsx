import { useState } from "react";
import { CheckCircle2, KeyRound, Loader2, Mail, Save, ShieldAlert } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { OperatorSectionCard } from "@/shared/design-system";
import type {
  BootstrapConfigConfirmationToken,
  BootstrapConfigMailSMTPValues,
  BootstrapConfigMailValues,
  BootstrapConfigResponse,
  BootstrapConfigSecretKey,
  BootstrapConfigValues,
} from "@/lib/types";
import {
  AUTH_FIELD_PATHS,
  MAIL_FIELD_PATHS,
  STATE_TRANSFER_FIELD_PATHS,
  getValidationStatusLabel,
  numberValue,
  textValue,
  type DangerousConfirmation,
  type FieldErrors,
  type SecretInputState,
  type SettingsStartupCopy,
  type ValidationRow,
} from "./startupFieldMetadata";
import {
  FieldLabelWithEffect,
  SecretReplacementField,
  StartupDisclosure,
  StartupInputField,
  type FieldEffectRenderer,
  type SectionEffectRenderer,
} from "./StartupServerSection";

interface StartupMailSecretsSectionProps {
  activeDangerousConfirmations: DangerousConfirmation[];
  bootstrapConfig: BootstrapConfigResponse;
  clearSecretInput: (secretKey: BootstrapConfigSecretKey) => void;
  confirmedTokens: BootstrapConfigConfirmationToken[];
  controlsDisabled: boolean;
  copy: SettingsStartupCopy;
  dangerDialogOpen: boolean;
  dangerousConfirmations: DangerousConfirmation[];
  dirtySummary: string[];
  fieldErrors: FieldErrors;
  fieldEffect: FieldEffectRenderer;
  handleDangerDialogOpenChange: (open: boolean) => void;
  handleSave: () => void;
  handleSecretInputChange: (secretKey: BootstrapConfigSecretKey, value: string) => void;
  handleValidate: () => Promise<void>;
  mailEnabled: boolean;
  mailValues: BootstrapConfigMailValues;
  performSave: () => Promise<void>;
  saving: boolean;
  sectionEffect: SectionEffectRenderer;
  secretInputs: SecretInputState;
  setBooleanField: (path: string, checked: boolean) => void;
  setMailEnabled: (checked: boolean) => void;
  setMailStringField: (field: "from" | "reply_to", rawValue: string) => void;
  setNumberField: (path: string, rawValue: string) => void;
  setSMTPNumberField: (field: keyof BootstrapConfigMailSMTPValues, rawValue: string) => void;
  setSMTPStringField: (field: keyof BootstrapConfigMailSMTPValues, rawValue: string) => void;
  setStringField: (path: string, rawValue: string) => void;
  smtpControlsDisabled: boolean;
  smtpValues: BootstrapConfigMailSMTPValues;
  toggleConfirmation: (token: BootstrapConfigConfirmationToken, checked: boolean) => void;
  validating: boolean;
  validationRows: ValidationRow[];
  values: BootstrapConfigValues;
  showReviewPanel?: boolean;
}

export function StartupMailSecretsSection({
  activeDangerousConfirmations,
  bootstrapConfig,
  clearSecretInput,
  confirmedTokens,
  controlsDisabled,
  copy,
  dangerDialogOpen,
  dangerousConfirmations,
  dirtySummary,
  fieldErrors,
  fieldEffect,
  handleDangerDialogOpenChange,
  handleSave,
  handleSecretInputChange,
  handleValidate,
  mailEnabled,
  mailValues,
  performSave,
  saving,
  sectionEffect,
  secretInputs,
  setBooleanField,
  setMailEnabled,
  setMailStringField,
  setNumberField,
  setSMTPNumberField,
  setSMTPStringField,
  setStringField,
  smtpControlsDisabled,
  smtpValues,
  toggleConfirmation,
  validating,
  validationRows,
  values,
  showReviewPanel = true,
}: StartupMailSecretsSectionProps) {
  const advancedSmtpActive = mailEnabled && (
    smtpValues.auth === "plain"
    || Boolean(smtpValues.ehlo_hostname)
    || Boolean(smtpValues.username)
    || Boolean(smtpValues.password_file)
    || Boolean(smtpValues.tls_server_name)
    || Boolean(secretInputs["mail.smtp.password"].trim())
    || Boolean(fieldErrors["mail.smtp.auth"])
    || Boolean(fieldErrors["mail.smtp.username"])
    || Boolean(fieldErrors["mail.smtp.password_file"])
    || Boolean(fieldErrors["mail.smtp.password"])
    || Boolean(fieldErrors["mail.smtp.timeout"])
  );
  const [advancedSmtpOpenOverride, setAdvancedSmtpOpenOverride] = useState<boolean | null>(null);
  const advancedSmtpOpen = advancedSmtpOpenOverride ?? advancedSmtpActive;

  return (
    <>
      <OperatorSectionCard
        icon={<KeyRound />}
        title={(
          <span className="flex flex-wrap items-center gap-2">
            {copy.authAndCookiesTitle}
            {sectionEffect(AUTH_FIELD_PATHS)}
          </span>
        )}
        description={copy.authAndCookiesDescription}
      >
          <FieldSet disabled={controlsDisabled}>
            <FieldLegend>{copy.auth}</FieldLegend>
            <FieldGroup>
              <SecretReplacementField
                id="startup-jwt-key"
                label={copy.jwtSigningKey}
                effect={fieldEffect("auth.jwtSigningKey")}
                secretKey="auth.jwtSigningKey"
                masked={bootstrapConfig.secrets["auth.jwtSigningKey"].masked}
                configured={bootstrapConfig.secrets["auth.jwtSigningKey"].configured}
                editable={bootstrapConfig.secrets["auth.jwtSigningKey"].editable && !controlsDisabled}
                value={secretInputs["auth.jwtSigningKey"]}
                copy={copy}
                onChange={handleSecretInputChange}
                onClear={clearSecretInput}
              />
              <div className="grid gap-4 md:grid-cols-2">
                <StartupInputField
                  id="startup-access-ttl"
                  label={copy.accessTokenTtlSeconds}
                  effect={fieldEffect("auth.access_token_ttl_seconds")}
                  type="number"
                  value={numberValue(values.auth.access_token_ttl_seconds)}
                  error={fieldErrors["auth.access_token_ttl_seconds"]}
                  disabled={controlsDisabled}
                  onChange={(value) => setNumberField("auth.access_token_ttl_seconds", value)}
                />
                <StartupInputField
                  id="startup-refresh-ttl"
                  label={copy.refreshTokenTtlSeconds}
                  effect={fieldEffect("auth.refresh_token_ttl_seconds")}
                  type="number"
                  value={numberValue(values.auth.refresh_token_ttl_seconds)}
                  error={fieldErrors["auth.refresh_token_ttl_seconds"]}
                  disabled={controlsDisabled}
                  onChange={(value) => setNumberField("auth.refresh_token_ttl_seconds", value)}
                />
                <StartupInputField
                  id="startup-reset-ttl"
                  label={copy.resetCodeTtlSeconds}
                  effect={fieldEffect("auth.reset_code_ttl_seconds")}
                  type="number"
                  value={numberValue(values.auth.reset_code_ttl_seconds)}
                  error={fieldErrors["auth.reset_code_ttl_seconds"]}
                  disabled={controlsDisabled}
                  onChange={(value) => setNumberField("auth.reset_code_ttl_seconds", value)}
                />
                <StartupInputField
                  id="startup-access-cookie"
                  label={copy.accessCookieName}
                  effect={fieldEffect("auth.access_cookie_name")}
                  value={textValue(values.auth.access_cookie_name)}
                  error={fieldErrors["auth.access_cookie_name"]}
                  disabled={controlsDisabled}
                  onChange={(value) => setStringField("auth.access_cookie_name", value)}
                />
                <StartupInputField
                  id="startup-refresh-cookie"
                  label={copy.refreshCookieName}
                  effect={fieldEffect("auth.refresh_cookie_name")}
                  value={textValue(values.auth.refresh_cookie_name)}
                  error={fieldErrors["auth.refresh_cookie_name"]}
                  disabled={controlsDisabled}
                  onChange={(value) => setStringField("auth.refresh_cookie_name", value)}
                />
              </div>
              <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
                <FieldContent>
                  <FieldLabelWithEffect htmlFor="startup-cookie-secure" label={copy.secureCookies} effect={fieldEffect("auth.cookie_secure")} />
                  <FieldDescription>{copy.secureCookiesDescription}</FieldDescription>
                </FieldContent>
                <Switch id="startup-cookie-secure" checked={Boolean(values.auth.cookie_secure)} disabled={controlsDisabled} onCheckedChange={(checked) => setBooleanField("auth.cookie_secure", checked)} />
              </Field>
            </FieldGroup>
          </FieldSet>
      </OperatorSectionCard>

      <OperatorSectionCard
        icon={<Mail />}
        title={(
          <span className="flex flex-wrap items-center gap-2">
            {copy.mailAndSmtpTitle}
            {sectionEffect(MAIL_FIELD_PATHS)}
          </span>
        )}
        description={copy.mailAndSmtpDescription}
        contentClassName="flex flex-col gap-6"
      >
          <FieldSet disabled={controlsDisabled}>
            <FieldLegend>{copy.mail}</FieldLegend>
            <FieldGroup>
              <Field orientation="horizontal" data-disabled={controlsDisabled || undefined}>
                <FieldContent>
                  <FieldLabelWithEffect htmlFor="startup-mail-enabled" label={copy.mailEnabled} />
                  <FieldDescription>{copy.mailEnabledDescription}</FieldDescription>
                </FieldContent>
                <Switch id="startup-mail-enabled" checked={mailEnabled} disabled={controlsDisabled} onCheckedChange={setMailEnabled} />
              </Field>
              <div className="grid gap-4 md:grid-cols-2">
                <StartupInputField
                  id="startup-mail-from"
                  label={copy.mailFrom}
                  value={textValue(mailValues.from)}
                  placeholder={copy.mailFromPlaceholder}
                  error={fieldErrors["mail.from"]}
                  disabled={smtpControlsDisabled}
                  onChange={(value) => setMailStringField("from", value)}
                />
                <StartupInputField
                  id="startup-mail-reply-to"
                  label={copy.mailReplyTo}
                  value={textValue(mailValues.reply_to)}
                  placeholder={copy.mailReplyToPlaceholder}
                  disabled={smtpControlsDisabled}
                  onChange={(value) => setMailStringField("reply_to", value)}
                />
              </div>
            </FieldGroup>
          </FieldSet>
          <Separator />
          <FieldSet disabled={smtpControlsDisabled}>
            <FieldLegend>{copy.smtp}</FieldLegend>
            <FieldDescription>{mailEnabled ? copy.smtpDescription : copy.smtpDisabledDescription}</FieldDescription>
            <FieldGroup>
              <div className="grid gap-4 md:grid-cols-2">
                <StartupInputField
                  id="startup-smtp-host"
                  label={copy.smtpHost}
                  value={textValue(smtpValues.host)}
                  placeholder={copy.smtpHostPlaceholder}
                  error={fieldErrors["mail.smtp.host"]}
                  disabled={smtpControlsDisabled}
                  onChange={(value) => setSMTPStringField("host", value)}
                />
                <StartupInputField
                  id="startup-smtp-port"
                  label={copy.smtpPort}
                  type="number"
                  value={numberValue(smtpValues.port)}
                  error={fieldErrors["mail.smtp.port"]}
                  disabled={smtpControlsDisabled}
                  onChange={(value) => setSMTPNumberField("port", value)}
                />
                <Field data-invalid={Boolean(fieldErrors["mail.smtp.mode"]) || undefined} data-disabled={smtpControlsDisabled || undefined}>
                  <FieldLabelWithEffect htmlFor="startup-smtp-mode" label={copy.smtpMode} />
                  <Select value={smtpValues.mode ?? ""} disabled={smtpControlsDisabled} onValueChange={(value) => setSMTPStringField("mode", value)}>
                    <SelectTrigger id="startup-smtp-mode" aria-invalid={Boolean(fieldErrors["mail.smtp.mode"]) || undefined}>
                      <SelectValue placeholder={copy.selectMode} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="starttls_required">{copy.smtpModeStarttlsRequired}</SelectItem>
                        <SelectItem value="implicit_tls">{copy.smtpModeImplicitTls}</SelectItem>
                        <SelectItem value="plaintext_local_only">{copy.smtpModePlaintextLocalOnly}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldError>{fieldErrors["mail.smtp.mode"]}</FieldError>
                </Field>
              </div>
            </FieldGroup>
          </FieldSet>
          <StartupDisclosure
            testId="startup-smtp-advanced-toggle"
            open={advancedSmtpOpen}
            onOpenChange={setAdvancedSmtpOpenOverride}
            closedLabel={copy.showAdvancedSmtp}
            openLabel={copy.hideAdvancedSmtp}
            description={copy.advancedSmtpDescription}
          >
            <div className="flex flex-col gap-4">
              <div className="grid gap-4 md:grid-cols-2">
                <StartupInputField
                  id="startup-smtp-ehlo"
                  label={copy.smtpEhloHostname}
                  value={textValue(smtpValues.ehlo_hostname)}
                  placeholder={copy.smtpEhloHostnamePlaceholder}
                  disabled={smtpControlsDisabled}
                  onChange={(value) => setSMTPStringField("ehlo_hostname", value)}
                />
                <Field data-invalid={Boolean(fieldErrors["mail.smtp.auth"]) || undefined} data-disabled={smtpControlsDisabled || undefined}>
                  <FieldLabelWithEffect htmlFor="startup-smtp-auth" label={copy.smtpAuth} />
                  <Select value={smtpValues.auth ?? ""} disabled={smtpControlsDisabled} onValueChange={(value) => setSMTPStringField("auth", value)}>
                    <SelectTrigger id="startup-smtp-auth" aria-invalid={Boolean(fieldErrors["mail.smtp.auth"]) || undefined}>
                      <SelectValue placeholder={copy.smtpAuthPlaceholder} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="none">{copy.smtpAuthNone}</SelectItem>
                        <SelectItem value="plain">{copy.smtpAuthPlain}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldError>{fieldErrors["mail.smtp.auth"]}</FieldError>
                </Field>
                <StartupInputField
                  id="startup-smtp-username"
                  label={copy.smtpUsername}
                  value={textValue(smtpValues.username)}
                  placeholder={copy.smtpUsernamePlaceholder}
                  error={fieldErrors["mail.smtp.username"]}
                  disabled={smtpControlsDisabled || smtpValues.auth !== "plain"}
                  onChange={(value) => setSMTPStringField("username", value)}
                />
                <StartupInputField
                  id="startup-smtp-password-file"
                  label={copy.smtpPasswordFile}
                  value={textValue(smtpValues.password_file)}
                  placeholder={copy.smtpPasswordFilePlaceholder}
                  description={copy.smtpPasswordFileDescription}
                  error={fieldErrors["mail.smtp.password_file"]}
                  disabled={smtpControlsDisabled}
                  onChange={(value) => setSMTPStringField("password_file", value)}
                />
                <StartupInputField
                  id="startup-smtp-timeout"
                  label={copy.smtpTimeout}
                  value={textValue(smtpValues.timeout)}
                  placeholder={copy.smtpTimeoutPlaceholder}
                  error={fieldErrors["mail.smtp.timeout"]}
                  disabled={smtpControlsDisabled}
                  onChange={(value) => setSMTPStringField("timeout", value)}
                />
                <StartupInputField
                  id="startup-smtp-tls-server-name"
                  label={copy.smtpTlsServerName}
                  value={textValue(smtpValues.tls_server_name)}
                  placeholder={copy.smtpTlsServerNamePlaceholder}
                  disabled={smtpControlsDisabled}
                  onChange={(value) => setSMTPStringField("tls_server_name", value)}
                />
              </div>
              <SecretReplacementField
                id="startup-smtp-password"
                label={copy.smtpPassword}
                secretKey="mail.smtp.password"
                masked={bootstrapConfig.secrets["mail.smtp.password"].masked}
                configured={bootstrapConfig.secrets["mail.smtp.password"].configured}
                editable={mailEnabled && bootstrapConfig.secrets["mail.smtp.password"].editable && !controlsDisabled}
                value={secretInputs["mail.smtp.password"]}
                copy={copy}
                error={fieldErrors["mail.smtp.password"]}
                onChange={handleSecretInputChange}
                onClear={clearSecretInput}
              />
            </div>
          </StartupDisclosure>
      </OperatorSectionCard>

      <OperatorSectionCard
        icon={<ShieldAlert />}
        title={(
          <span className="flex flex-wrap items-center gap-2">
            {copy.stateTransferTitle}
            {sectionEffect(STATE_TRANSFER_FIELD_PATHS)}
          </span>
        )}
        description={copy.stateTransferDescription}
      >
          <FieldSet disabled={controlsDisabled}>
            <FieldLegend>{copy.secrets}</FieldLegend>
            <FieldGroup>
              <SecretReplacementField
                id="startup-bundle-key"
                label={copy.bundleEncryptionKey}
                secretKey="stateTransfer.bundleEncryptionKey"
                masked={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].masked}
                configured={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].configured}
                editable={bootstrapConfig.secrets["stateTransfer.bundleEncryptionKey"].editable && !controlsDisabled}
                value={secretInputs["stateTransfer.bundleEncryptionKey"]}
                copy={copy}
                onChange={handleSecretInputChange}
                onClear={clearSecretInput}
              />
              <SecretReplacementField
                id="startup-runtime-secret-key"
                label={copy.runtimeSecretEncryptionKey}
                secretKey="runtime.secretEncryptionKey"
                masked={bootstrapConfig.secrets["runtime.secretEncryptionKey"].masked}
                configured={bootstrapConfig.secrets["runtime.secretEncryptionKey"].configured}
                editable={false}
                value=""
                copy={copy}
                onChange={handleSecretInputChange}
                onClear={clearSecretInput}
              />
            </FieldGroup>
          </FieldSet>
      </OperatorSectionCard>

      {showReviewPanel ? (
        <OperatorSectionCard
          icon={<CheckCircle2 />}
          title={copy.reviewAndSaveTitle}
          description={copy.reviewAndSaveDescription}
          contentClassName="flex flex-col gap-4"
          footer={(
            <div className="flex w-full justify-end gap-2">
              <Button type="button" variant="outline" disabled={controlsDisabled} onClick={() => void handleValidate()}>
                {validating ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <CheckCircle2 data-icon="inline-start" />}
                {copy.validate}
              </Button>
              <Button type="button" disabled={controlsDisabled || saving || !bootstrapConfig.writable} onClick={handleSave}>
                {saving ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <Save data-icon="inline-start" />}
                {copy.saveStartupConfig}
              </Button>
            </div>
          )}
        >
            <div className="flex flex-wrap gap-2">
              {dirtySummary.length ? dirtySummary.map((item) => <Badge key={item} variant="secondary">{item}</Badge>) : <Badge variant="outline">{copy.noLocalChangesDetected}</Badge>}
              {activeDangerousConfirmations.length ? <Badge variant="destructive">{copy.dangerousChangesStaged}</Badge> : null}
            </div>
            <FieldSet>
              <FieldLegend>{copy.dangerousChecklistTitle}</FieldLegend>
              <FieldDescription>{copy.dangerousChecklistDescription}</FieldDescription>
              <FieldGroup data-slot="checkbox-group">
                {dangerousConfirmations.map((confirmation) => (
                  <Field key={confirmation.token} orientation="horizontal" data-disabled={!confirmation.active || controlsDisabled || undefined}>
                    <Checkbox
                      id={confirmation.token}
                      checked={confirmedTokens.includes(confirmation.token as BootstrapConfigConfirmationToken)}
                      disabled={!confirmation.active || controlsDisabled}
                      onCheckedChange={(checked) => toggleConfirmation(confirmation.token as BootstrapConfigConfirmationToken, checked === true)}
                    />
                    <FieldContent>
                      <FieldLabel htmlFor={confirmation.token}>{confirmation.label}</FieldLabel>
                      <FieldDescription>{confirmation.active ? copy.confirmationRequiredBeforeSave : copy.noChangeCurrentlyStaged}</FieldDescription>
                    </FieldContent>
                  </Field>
                ))}
              </FieldGroup>
            </FieldSet>
            <Separator />
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
                    <TableCell><Badge variant={row.status === "error" ? "destructive" : row.status === "warning" ? "secondary" : "outline"}>{getValidationStatusLabel(row.status, copy)}</Badge></TableCell>
                    <TableCell>{row.field}</TableCell>
                    <TableCell className="whitespace-normal">{row.message}</TableCell>
                  </TableRow>
                ))}
                {!validationRows.length ? <TableRow><TableCell colSpan={3} className="text-muted-foreground">{copy.noValidationRunYet}</TableCell></TableRow> : null}
              </TableBody>
            </Table>
        </OperatorSectionCard>
      ) : null}

      <AlertDialog open={dangerDialogOpen} onOpenChange={handleDangerDialogOpenChange}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{copy.dangerDialogTitle}</AlertDialogTitle>
            <AlertDialogDescription>{copy.dangerDialogDescription}</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="rounded-lg border border-outline-variant bg-surface-container-low p-3 text-sm">
            <ul className="flex list-disc flex-col gap-1 pl-5">
              {activeDangerousConfirmations.map((confirmation) => <li key={confirmation.token}>{confirmation.label}</li>)}
            </ul>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={saving}>{copy.saveDangerousChangesCancel}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={saving}
              onClick={(event) => {
                event.preventDefault();
                void performSave();
              }}
            >
              {copy.saveAndRequireRestart}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
