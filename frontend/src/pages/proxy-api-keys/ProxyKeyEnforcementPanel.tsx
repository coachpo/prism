import { ShieldAlert, ShieldCheck } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { useLocale } from "@/i18n/useLocale";
import type { AuthSettings } from "@/lib/types";
import { OperatorCallout, OperatorInsetPanel, OperatorSectionCard } from "@/shared/design-system";
import { getProxyKeyUsagePercent } from "./proxyKeyFormatting";

interface ProxyKeyEnforcementPanelProps {
  authEnabled: boolean;
  authSettings: AuthSettings | null;
  proxyKeyLimit: number;
  proxyKeysUsed: number;
  remainingKeys: number;
}

export function ProxyKeyEnforcementPanel({
  authEnabled,
  authSettings,
  proxyKeyLimit,
  proxyKeysUsed,
  remainingKeys,
}: ProxyKeyEnforcementPanelProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const quotaPercent = getProxyKeyUsagePercent(proxyKeysUsed, proxyKeyLimit);
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
      />

      <OperatorInsetPanel className="bg-surface">
        <div className="flex items-center justify-between gap-3">
          <div className="flex flex-col gap-1">
            <p className="text-sm font-medium">{copy.issuedKeys}</p>
            <p className="text-sm text-muted-foreground">
              {copy.keysUsed(formatNumber(proxyKeysUsed), formatNumber(proxyKeyLimit))}
            </p>
          </div>
          <Badge variant={remainingKeys === 0 ? "destructive" : "outline"}>
            {remainingKeys === 0 ? copy.keyLimitReached : copy.slotsRemaining(formatNumber(remainingKeys))}
          </Badge>
        </div>
        <Progress value={quotaPercent} />
      </OperatorInsetPanel>
    </OperatorSectionCard>
  );
}
