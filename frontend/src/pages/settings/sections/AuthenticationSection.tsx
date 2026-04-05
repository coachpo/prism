import { Shield } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { AuthenticationSetupGrid } from "./authentication/AuthenticationSetupGrid";
import { AuthenticationStatusCard } from "./authentication/AuthenticationStatusCard";
import { PasskeysCard } from "./authentication/PasskeysCard";
import type { AuthenticationSectionProps } from "./authentication/types";

export function AuthenticationSection({
  authEnabled,
  authSettings,
  authSaving,
  password,
  passwordError,
  passwordMismatch,
  username,
  ...props
}: AuthenticationSectionProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuthentication;
  const usernameReady = username.trim().length > 0;
  const passwordReady = authSettings?.has_password
    ? !passwordError && !passwordMismatch
    : Boolean(password) && !passwordError && !passwordMismatch;
  const emailReady = Boolean(
    authSettings?.email && authSettings.email_bound_at && !authSettings.pending_email
  );
  const setupReady = usernameReady && emailReady && passwordReady;
  const statusDescription = authEnabled
    ? copy.proxyKeyTrafficRequirement
    : copy.authenticationDisabledDescription;

  return (
    <section id="authentication" tabIndex={-1} className="scroll-mt-24">
      <Card>
        <CardHeader className="pb-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-sm">
                <Shield className="h-4 w-4" />
                {copy.authentication}
              </CardTitle>
              <CardDescription className="text-xs">
                {copy.authenticationDisabledDescription}
              </CardDescription>
            </div>
            <Badge variant={authEnabled ? "default" : "outline"} className="w-fit">
              {authEnabled ? messages.loadbalanceStrategiesTable.enabled : messages.loadbalanceStrategiesTable.disabled}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          <AuthenticationStatusCard
            authEnabled={authEnabled}
            authSaving={authSaving}
            setupReady={setupReady}
            statusDescription={statusDescription}
            onSaveAuthSettings={props.onSaveAuthSettings}
          />

          <AuthenticationSetupGrid
            authEnabled={authEnabled}
            authSaving={authSaving}
            authSettings={authSettings}
            password={password}
            passwordError={passwordError}
            passwordMismatch={passwordMismatch}
            username={username}
            {...props}
          />

          <PasskeysCard authEnabled={authEnabled} />
        </CardContent>
      </Card>
    </section>
  );
}
