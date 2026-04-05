import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { Input } from "@/components/ui/input";
import { AuthenticationFieldShell } from "./AuthenticationFieldShell";
import type { AuthenticationSectionProps } from "./types";

type OperatorEmailCardProps = Pick<
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

export function OperatorEmailCard({
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
}: OperatorEmailCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuthentication;
  return (
    <Card className="shadow-none">
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{copy.operatorAccount}</CardTitle>
        <CardDescription>
          {copy.operatorAccountDescription}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <AuthenticationFieldShell
          label={copy.username}
          helper={copy.usernameHelper}
          htmlFor="auth-username"
        >
          <Input
            id="auth-username"
            name="username"
            autoComplete="off"
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
                : messages.settingsAuthentication.enableAuthenticationToManagePasskeys
          }
          helperClassName={passwordError ? "text-destructive" : undefined}
          htmlFor="auth-password"
        >
          <Input
            id="auth-password"
            name="password"
            type="password"
            autoComplete="off"
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
          htmlFor="auth-password-confirm"
        >
          <Input
            id="auth-password-confirm"
            name="password-confirm"
            type="password"
            autoComplete="off"
            value={passwordConfirm}
            onChange={(event) => setPasswordConfirm(event.target.value)}
          />
        </AuthenticationFieldShell>

        <div className="rounded-lg border bg-muted/30 px-4 py-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <p className="text-xs text-muted-foreground">
                {copy.authenticationToggleDescription}
              </p>
            <Button
              type="button"
              variant="outline"
              onClick={() => void onSaveAuthSettings(authEnabled)}
              disabled={authSaving || Boolean(passwordError) || passwordMismatch}
            >
              {authSaving ? messages.pricingTemplateDialog.saving : copy.saveAccountChanges}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
