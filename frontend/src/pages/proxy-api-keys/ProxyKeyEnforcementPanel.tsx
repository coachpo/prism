import { ShieldAlert, ShieldCheck } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useLocale } from "@/i18n/useLocale";
import type { AuthSettings } from "@/lib/types";
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
    <Card className="h-full">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base">
          {authEnabled ? <ShieldCheck className="text-success" /> : <ShieldAlert className="text-warning" />}
          {statusLabel}
        </CardTitle>
        <CardDescription>{statusDescription}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Alert className="bg-muted/20">
          {authEnabled ? <ShieldCheck /> : <ShieldAlert />}
          <AlertTitle>{statusLabel}</AlertTitle>
          <AlertDescription>
            {authSettings
              ? authEnabled
                ? messages.settingsAuthentication.proxyKeyTrafficRequirement
                : messages.settingsAuthentication.enableAuthenticationToEnforceKeys
              : statusDescription}
          </AlertDescription>
        </Alert>

        <div className="flex flex-col gap-3 rounded-lg border p-3">
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
        </div>
      </CardContent>
    </Card>
  );
}
