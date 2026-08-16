import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";
import type { AuthSettings } from "@/lib/types";
import { OperatorStatusBadge } from "@/shared/design-system";
import { getAuthStatusTier, isAuthSettingsEnabled } from "./proxyKeyFormatting";

interface ProxyKeyEnforcementPanelProps {
  authSettings: AuthSettings | null;
  loading: boolean;
}

/**
 * Enforcement state appears exactly once on this page. It was previously
 * repeated three times — page header badge, card title, callout title — inside
 * a 20rem column that carried no other information.
 */
export function ProxyKeyEnforcementPanel({ authSettings, loading }: ProxyKeyEnforcementPanelProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const authEnabled = isAuthSettingsEnabled(authSettings);

  // A read still in flight is not the same fact as "unavailable", so the bar
  // holds its shape instead of claiming enforcement is broken.
  if (loading) {
    return (
      <section
        aria-busy="true"
        className="operator-section-surface flex min-w-0 items-center gap-3 rounded-lg border px-[var(--density-card-pad-x)] py-2"
      >
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-4 max-w-lg flex-1" />
      </section>
    );
  }

  const statusLabel = authSettings
    ? authEnabled
      ? copy.authenticationOn
      : copy.authenticationOff
    : copy.authenticationUnavailable;
  const statusDescription = authSettings
    ? authEnabled
      ? messages.settingsAuthentication.proxyKeyTrafficRequirement
      : messages.settingsAuthentication.enableAuthenticationToEnforceKeys
    : messages.proxyApiKeysData.settingsUnavailable;

  return (
    <section
      data-testid="proxy-key-enforcement"
      className="operator-section-surface flex min-w-0 flex-col gap-2 rounded-lg border px-[var(--density-card-pad-x)] py-2 sm:flex-row sm:items-center sm:gap-3"
    >
      <OperatorStatusBadge intent={getAuthStatusTier(authSettings)} label={statusLabel} preserveLabel />
      <p className="min-w-0 flex-1 text-xs text-muted-foreground">
        {statusDescription}
        <span className="ml-1">{copy.scopeDescription}</span>
      </p>
      <Button asChild variant="outline" size="sm" className="shrink-0">
        <Link to="/system/settings?scope=instance&section=authentication">
          {messages.settingsAuthentication.goToAuthenticationSettings}
        </Link>
      </Button>
    </section>
  );
}
