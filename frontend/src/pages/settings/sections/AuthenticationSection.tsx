import { Shield } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
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
import { Link } from "@tanstack/react-router";
import {
  OperatorInsetPanel,
  OperatorSectionCard,
  OperatorStatusBadge,
  OperatorSwitchField,
} from "@/shared/design-system";
import { OperatorAccountFields } from "./authentication/OperatorAccountFields";
import type { AuthenticationSectionProps } from "./authentication/types";

/**
 * One card, not three.
 *
 * This section used to nest a card titled `身份验证` inside a card titled
 * `身份验证`, plus a third for the operator account, so the same word appeared
 * three times before any actual state did. The card header now carries the
 * three facts that answer "what is enforcing right now": enabled state,
 * effective generation, and the current operator.
 */
export function AuthenticationSection({
  authEnabled,
  authSettings,
  authSaving,
  password,
  passwordError,
  passwordMismatch,
  username,
  pendingAuthConfirmation,
  onConfirmAuthSettings,
  onCancelAuthSettingsConfirmation,
  ...props
}: AuthenticationSectionProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuthentication;
  const usernameReady = username.trim().length > 0;
  const passwordReady = authSettings?.has_password
    ? !passwordError && !passwordMismatch
    : Boolean(password) && !passwordError && !passwordMismatch;
  const setupReady = usernameReady && passwordReady;
  const statusDescription = authEnabled
    ? copy.proxyKeyTrafficRequirement
    : copy.authenticationDisabledRisk;
  const readiness = authSettings?.proxy_key_readiness;
  const readinessDescription = readiness?.state === "ready"
    ? copy.authenticationReadiness(
        readiness.active ?? "0",
        readiness.activation_guard?.safe_active ?? "0",
        readiness.expired ?? "0",
        readiness.disabled ?? "0",
      )
    : copy.readinessUnavailable;
  const effectiveGeneration = authSettings?.auth_mode?.effective_generation;
  const operatorUsername = authSettings?.operator_account?.effective.username;

  return (
    <section id="authentication" tabIndex={-1} className="scroll-mt-24">
      <OperatorSectionCard
        title={(
          <span className="flex items-center gap-2">
            <Shield data-icon="inline-start" />
            {copy.authentication}
          </span>
        )}
        description={statusDescription}
        actions={(
          <div className="flex flex-wrap items-center justify-end gap-2 text-xs text-muted-foreground">
            <OperatorStatusBadge
              intent={authEnabled ? "healthy" : "degraded"}
              label={authEnabled ? messages.loadbalanceStrategiesTable.enabled : messages.loadbalanceStrategiesTable.disabled}
              preserveLabel
            />
            <span className="font-mono tabular-nums">
              {effectiveGeneration
                ? copy.effectiveGeneration(effectiveGeneration)
                : copy.effectiveGenerationUnknown}
            </span>
            <span aria-hidden="true">·</span>
            <span>
              {operatorUsername ? copy.currentOperator(operatorUsername) : copy.currentOperatorUnconfigured}
            </span>
          </div>
        )}
        contentClassName="flex flex-col gap-4"
      >
        <OperatorSwitchField
          label={copy.authentication}
          description={copy.authenticationToggleDescription}
          checked={authEnabled}
          disabled={authSaving || (!setupReady && !authEnabled)}
          onCheckedChange={(checked) => {
            void props.onSaveAuthSettings(checked);
          }}
          className="border-border bg-inset"
        />

        <OperatorInsetPanel
          title={copy.proxyKeyReadinessTitle}
          description={copy.attributionDescription}
          actions={(
            <Button asChild type="button" variant="outline" size="sm">
              <Link to="/system/proxy-keys">{copy.manageProxyKeys}</Link>
            </Button>
          )}
        >
          <p className={readiness?.state === "ready" ? "text-xs text-muted-foreground" : "text-xs text-degraded"}>
            {readinessDescription}
          </p>
        </OperatorInsetPanel>

        <OperatorInsetPanel title={copy.operatorAccount} description={copy.operatorAccountDescription}>
          <OperatorAccountFields
            authEnabled={authEnabled}
            authSaving={authSaving}
            authSettings={authSettings}
            password={password}
            passwordConfirm={props.passwordConfirm}
            passwordError={passwordError}
            passwordMismatch={passwordMismatch}
            onSaveAuthSettings={props.onSaveAuthSettings}
            setPassword={props.setPassword}
            setPasswordConfirm={props.setPasswordConfirm}
            setUsername={props.setUsername}
            username={username}
          />
        </OperatorInsetPanel>
      </OperatorSectionCard>

      <AlertDialog
        open={pendingAuthConfirmation !== null}
        onOpenChange={(open) => {
          if (!open) {
            onCancelAuthSettingsConfirmation();
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{copy.confirmationTitle}</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingAuthConfirmation === "disable"
                ? copy.disableConfirmation
                : pendingAuthConfirmation === "account_update"
                  ? copy.accountUpdateConfirmation
                  : copy.zeroKeyEnableConfirmation}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{messages.settingsDialogs.cancel}</AlertDialogCancel>
            <AlertDialogAction
              variant={pendingAuthConfirmation === "disable" ? "destructive" : "default"}
              onClick={() => {
                void onConfirmAuthSettings();
              }}
            >
              {copy.continue}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
