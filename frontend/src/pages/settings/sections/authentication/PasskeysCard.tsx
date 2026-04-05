import { Fingerprint, Plus, Trash2 } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { buildPasskeyMetadata, getPasskeyStateBadge } from "./passkeyMetadata";
import { PasskeyRegisterDialog } from "./PasskeyRegisterDialog";
import { PasskeyRemoveDialog } from "./PasskeyRemoveDialog";
import { usePasskeyManagement } from "./usePasskeyManagement";

export function PasskeysCard({ authEnabled }: { authEnabled: boolean }) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.settingsAuthentication;
  const {
    deviceName,
    handleRegisterClick,
    handleRegisterSubmit,
    handleRemoveClick,
    handleRemoveConfirm,
    isPasskeySupported,
    loadingPasskeys,
    passkeyToRemove,
    passkeys,
    registerDialogOpen,
    registering,
    removeDialogOpen,
    removing,
    setDeviceName,
    setRegisterDialogOpen,
    setRemoveDialogOpen,
  } = usePasskeyManagement(authEnabled);

  return (
    <>
      <Card className="shadow-none">
        <CardHeader className="gap-4 pb-4">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Fingerprint className="h-4 w-4" />
                  {copy.passkeys}
                </CardTitle>
                <Badge
                  variant="outline"
                  className="rounded-full px-2.5 py-0.5 text-[11px] font-medium text-muted-foreground"
                >
                  {copy.passkeysRegistered(formatNumber(passkeys.length))}
                </Badge>
              </div>
              <CardDescription className="max-w-2xl text-sm leading-6">
                {messages.auth.signInWithPasskey}
              </CardDescription>
            </div>

            <Button
              type="button"
              onClick={handleRegisterClick}
              disabled={!authEnabled || !isPasskeySupported || loadingPasskeys || registering}
              className="h-9 w-9 rounded-full p-0 sm:w-auto sm:rounded-md sm:px-4"
            >
              <Plus className="h-4 w-4 shrink-0 sm:mr-2" />
              <span className="hidden sm:inline">{copy.addPasskey}</span>
              <span className="sr-only sm:hidden">{copy.addPasskey}</span>
            </Button>
          </div>
        </CardHeader>

        <CardContent className="space-y-4 pt-0">
          {!authEnabled ? (
            <div className="flex flex-col items-center justify-center rounded-xl border border-dashed px-4 py-10 text-center">
              <Fingerprint className="mb-3 h-8 w-8 text-muted-foreground/50" />
              <p className="text-sm font-medium">{copy.authenticationIsDisabled}</p>
              <p className="mt-1 max-w-sm text-sm text-muted-foreground">
                {copy.enableAuthenticationToManagePasskeys}
              </p>
            </div>
          ) : !isPasskeySupported ? (
            <div className="rounded-lg border border-destructive/25 bg-destructive/10 p-4">
              <p className="text-sm text-destructive">
                {copy.unsupportedPasskeys}
              </p>
            </div>
          ) : loadingPasskeys ? (
            <div className="rounded-xl border border-dashed px-4 py-10 text-center">
              <p className="text-sm text-muted-foreground">{messages.settingsAuditRules.loadingRules}</p>
            </div>
          ) : passkeys.length === 0 ? (
            <div className="flex flex-col items-center justify-center rounded-xl border border-dashed px-4 py-10 text-center">
              <Fingerprint className="mb-3 h-8 w-8 text-muted-foreground/50" />
              <p className="text-sm font-medium">{copy.noPasskeysRegistered}</p>
              <p className="mt-1 max-w-sm text-sm text-muted-foreground">
                {copy.noPasskeysRegisteredDescription}
              </p>
            </div>
          ) : (
            <div className="overflow-hidden rounded-xl border border-border/70 bg-background/70">
              <div className="divide-y divide-border/70">
                {passkeys.map((passkey) => {
                  const passkeyName = passkey.device_name || copy.passkeyFallbackName(passkey.id);
                  const stateBadge = getPasskeyStateBadge(passkey);

                  return (
                    <div
                      key={passkey.id}
                      className="flex flex-col gap-4 px-4 py-4 sm:flex-row sm:items-center sm:justify-between"
                    >
                      <div className="flex min-w-0 flex-1 items-start gap-4">
                        <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl border border-border/70 bg-muted/50">
                          <Fingerprint className="h-5 w-5" />
                        </div>

                        <div className="min-w-0 space-y-2">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="truncate text-sm font-semibold text-foreground">
                              {passkeyName}
                            </p>
                            {stateBadge ? (
                              <Badge variant="outline" className={stateBadge.className}>
                                {stateBadge.label}
                              </Badge>
                            ) : null}
                          </div>

                          <p className="text-sm leading-6 text-muted-foreground">
                            {buildPasskeyMetadata(passkey)}
                          </p>
                        </div>
                      </div>

                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="h-9 w-9 shrink-0 rounded-full text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                        onClick={() => handleRemoveClick(passkey)}
                        aria-label={copy.removeItem(passkeyName)}
                      >
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <PasskeyRegisterDialog
        open={registerDialogOpen}
        onOpenChange={setRegisterDialogOpen}
        deviceName={deviceName}
        setDeviceName={setDeviceName}
        onSubmit={handleRegisterSubmit}
        registering={registering}
      />

      <PasskeyRemoveDialog
        open={removeDialogOpen}
        onOpenChange={setRemoveDialogOpen}
        passkeyToRemove={passkeyToRemove}
        onConfirmRemove={handleRemoveConfirm}
        removing={removing}
      />
    </>
  );
}
