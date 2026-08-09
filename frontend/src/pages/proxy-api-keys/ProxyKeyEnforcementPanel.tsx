import { ShieldAlert, ShieldCheck } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import type { AuthSettings } from "@/lib/types";
import { OperatorCallout, OperatorInsetPanel, OperatorSectionCard } from "@/shared/design-system";

interface ProxyKeyEnforcementPanelProps {
  authEnabled: boolean;
  authSettings: AuthSettings | null;
}

export function ProxyKeyEnforcementPanel({ authEnabled, authSettings }: ProxyKeyEnforcementPanelProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const statusLabel = authSettings
    ? authEnabled
      ? copy.authenticationOn
      : copy.authenticationOff
    : copy.authenticationUnavailable;
  const statusDescription = authSettings
    ? authEnabled
      ? copy.keysProtectedDescription
      : copy.keysPreparedDescription
    : messages.proxyApiKeysData.settingsUnavailable;

  return (
    <OperatorSectionCard
      className="h-full"
      icon={authEnabled ? <ShieldCheck className="text-success" /> : <ShieldAlert className="text-warning" />}
      title={statusLabel}
      description={statusDescription}
      contentClassName="flex flex-col gap-4"
    >
      <OperatorCallout
        intent={authEnabled ? "success" : "warning"}
        title={statusLabel}
        icon={authEnabled ? <ShieldCheck /> : <ShieldAlert />}
        description={
          authSettings
            ? authEnabled
              ? messages.settingsAuthentication.proxyKeyTrafficRequirement
              : messages.settingsAuthentication.enableAuthenticationToEnforceKeys
            : statusDescription
        }
        action={
          <Button asChild variant="outline" size="sm">
            <Link to="/system/settings?tab=global&section=authentication#authentication">
              {messages.settingsAuthentication.goToAuthenticationSettings}
            </Link>
          </Button>
        }
      />

      <OperatorInsetPanel className="bg-surface">
        <p className="text-sm font-medium">{copy.scopeTitle}</p>
        <p className="text-sm text-muted-foreground">{copy.scopeDescription}</p>
      </OperatorInsetPanel>
    </OperatorSectionCard>
  );
}
