import { ShieldAlert, ShieldCheck } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";

interface ProxyKeyStatusCalloutProps {
  authEnabled: boolean;
}

export function ProxyKeyStatusCallout({ authEnabled }: ProxyKeyStatusCalloutProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  return (
    <div
      className={
        authEnabled
          ? "rounded-lg border border-emerald-500/25 bg-emerald-500/10 p-4"
          : "rounded-lg border border-amber-500/25 bg-amber-500/10 p-4"
      }
    >
      <div className="flex items-start gap-3">
        {authEnabled ? (
          <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-emerald-700 dark:text-emerald-300" />
        ) : (
          <ShieldAlert className="mt-0.5 h-4 w-4 shrink-0 text-amber-700 dark:text-amber-300" />
        )}
        <div className="space-y-1">
          <p className="text-sm font-medium">
            {authEnabled
              ? copy.keysProtectedDescription
              : copy.keysPreparedDescription}
          </p>
          <p className="text-sm text-muted-foreground">
            {authEnabled
              ? messages.settingsAuthentication.proxyKeyTrafficRequirement
              : messages.settingsAuthentication.enableAuthenticationToEnforceKeys}
          </p>
        </div>
      </div>
    </div>
  );
}
