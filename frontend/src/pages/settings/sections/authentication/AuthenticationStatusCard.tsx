import { useLocale } from "@/i18n/useLocale";
import { OperatorSectionCard, OperatorSwitchField } from "@/shared/design-system";

interface AuthenticationStatusCardProps {
  authEnabled: boolean;
  authSaving: boolean;
  setupReady: boolean;
  statusDescription: string;
  onSaveAuthSettings: (nextEnabled?: boolean) => Promise<void>;
}

export function AuthenticationStatusCard({
  authEnabled,
  authSaving,
  setupReady,
  statusDescription,
  onSaveAuthSettings,
}: AuthenticationStatusCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuthentication;
  return (
    <OperatorSectionCard title={copy.authentication} description={statusDescription}>
      <OperatorSwitchField
        label={copy.authentication}
        description={copy.authenticationToggleDescription}
        checked={authEnabled}
        disabled={authSaving || (!setupReady && !authEnabled)}
        onCheckedChange={(checked) => {
          void onSaveAuthSettings(checked);
        }}
        className="border-outline-variant bg-surface-container-low"
      />
    </OperatorSectionCard>
  );
}
