import { Shield } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { OperatorSectionCard } from "@/shared/design-system";
import { AuthenticationSetupGrid } from "./authentication/AuthenticationSetupGrid";
import { AuthenticationStatusCard } from "./authentication/AuthenticationStatusCard";
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
  const setupReady = usernameReady && passwordReady;
  const statusDescription = authEnabled
    ? copy.proxyKeyTrafficRequirement
    : copy.authenticationDisabledDescription;

  return (
    <section id="authentication" tabIndex={-1} className="scroll-mt-24">
      <OperatorSectionCard
        title={(
          <span className="flex items-center gap-2">
            <Shield data-icon="inline-start" />
            {copy.authentication}
          </span>
        )}
        actions={(
          <Badge variant={authEnabled ? "default" : "outline"} className="w-fit">
            {authEnabled ? messages.loadbalanceStrategiesTable.enabled : messages.loadbalanceStrategiesTable.disabled}
          </Badge>
        )}
        contentClassName="flex flex-col gap-4"
      >
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
      </OperatorSectionCard>
    </section>
  );
}
