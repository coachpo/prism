import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { Input } from "@/components/ui/input";
import { AuthenticationFieldShell } from "./AuthenticationFieldShell";
import type { AuthenticationSectionProps } from "./types";

type OperatorAccountFieldsProps = Pick<
  AuthenticationSectionProps,
  | "authEnabled"
  | "authSaving"
  | "authSettings"
  | "onSaveAuthSettings"
  | "password"
  | "passwordConfirm"
  | "passwordError"
  | "passwordMismatch"
  | "setPassword"
  | "setPasswordConfirm"
  | "setUsername"
  | "username"
>;

/**
 * The operator credential form, without a card of its own: the authentication
 * section owns the single card and renders this inside an inset panel.
 */
export function OperatorAccountFields({
  authEnabled,
  authSaving,
  authSettings,
  onSaveAuthSettings,
  password,
  passwordConfirm,
  passwordError,
  passwordMismatch,
  setPassword,
  setPasswordConfirm,
  setUsername,
  username,
}: OperatorAccountFieldsProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuthentication;
  return (
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          void onSaveAuthSettings(authEnabled);
        }}
        noValidate
      >
        <AuthenticationFieldShell
          label={copy.username}
          helper={copy.usernameHelper}
          htmlFor="auth-username"
        >
          <Input
            id="auth-username"
            name="username"
            autoComplete="username"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            placeholder={copy.usernamePlaceholder}
          />
        </AuthenticationFieldShell>

        <AuthenticationFieldShell
          label={copy.password}
          helper={
            passwordError
              ? passwordError
              : authSettings?.has_password
                ? copy.passwordKeepCurrent
                : copy.authenticationToggleDescription
          }
          helperClassName={passwordError ? "text-destructive" : undefined}
          descriptionId={passwordError ? "auth-password-error" : undefined}
          htmlFor="auth-password"
        >
          <Input
            id="auth-password"
            name="password"
            type="password"
            autoComplete="new-password"
            aria-invalid={passwordError ? true : undefined}
            aria-describedby={passwordError ? "auth-password-error" : undefined}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </AuthenticationFieldShell>

        <AuthenticationFieldShell
          label={copy.confirmPassword}
          helper={
            passwordMismatch
              ? copy.passwordsMustMatch
              : copy.passwordConfirmationHelp
          }
          helperClassName={passwordMismatch ? "text-destructive" : undefined}
          descriptionId={passwordMismatch ? "auth-password-confirm-error" : undefined}
          htmlFor="auth-password-confirm"
        >
          <Input
            id="auth-password-confirm"
            name="password-confirm"
            type="password"
            autoComplete="new-password"
            aria-invalid={passwordMismatch ? true : undefined}
            aria-describedby={passwordMismatch ? "auth-password-confirm-error" : undefined}
            value={passwordConfirm}
            onChange={(event) => setPasswordConfirm(event.target.value)}
          />
        </AuthenticationFieldShell>

        <div className="flex justify-end">
          <Button
            type="submit"
            variant="outline"
            disabled={authSaving || Boolean(passwordError) || passwordMismatch}
          >
            {authSaving ? messages.pricingTemplateDialog.saving : copy.saveAccountChanges}
          </Button>
        </div>
      </form>
  );
}
