import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { SwitchController } from "@/components/SwitchController";

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
    <Card className="shadow-none">
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{copy.authentication}</CardTitle>
        <CardDescription>
          <span className="font-medium text-foreground">
            {copy.authentication}: {authEnabled ? messages.shell.activate : messages.settingsDialogs.cancel}
          </span>{" "}
          {statusDescription}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <SwitchController
          label={copy.authentication}
          description={copy.enableAuthenticationToManagePasskeys}
          checked={authEnabled}
          disabled={authSaving || (!setupReady && !authEnabled)}
          onCheckedChange={(checked) => {
            void onSaveAuthSettings(checked);
          }}
          className="border-border bg-muted/20"
        />
      </CardContent>
    </Card>
  );
}
