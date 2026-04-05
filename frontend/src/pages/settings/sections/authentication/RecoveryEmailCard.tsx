import { useState } from "react";
import { MailCheck } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { Input } from "@/components/ui/input";
import { AuthenticationFieldShell } from "./AuthenticationFieldShell";
import type { AuthenticationSectionProps } from "./types";

type RecoveryEmailCardProps = Pick<
  AuthenticationSectionProps,
  | "authSettings"
  | "confirmingEmailVerification"
  | "email"
  | "emailVerificationOtp"
  | "onConfirmEmailVerification"
  | "onRequestEmailVerification"
  | "sendingEmailVerification"
  | "setEmail"
  | "setEmailVerificationOtp"
>;

export function RecoveryEmailCard({
  authSettings,
  confirmingEmailVerification,
  email,
  emailVerificationOtp,
  onConfirmEmailVerification,
  onRequestEmailVerification,
  sendingEmailVerification,
  setEmail,
  setEmailVerificationOtp,
}: RecoveryEmailCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuthentication;
  const [emailEditorOpen, setEmailEditorOpen] = useState(false);
  const verificationPending = Boolean(authSettings?.pending_email);
  const verifiedEmail = authSettings?.email ?? null;
  const emailVerified = Boolean(
    verifiedEmail && authSettings?.email_bound_at && !verificationPending,
  );
  const emailChanged = Boolean(email.trim()) && email.trim() !== (verifiedEmail ?? "");
  const showEmailEditor = emailEditorOpen || verificationPending || emailChanged || !emailVerified;

  return (
    <Card className="shadow-none">
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{copy.recoveryEmail}</CardTitle>
        <CardDescription>
          {copy.recoveryEmailDescription}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {emailVerified && !showEmailEditor ? (
          <div className="rounded-lg border border-emerald-500/25 bg-emerald-500/10 p-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="space-y-1">
                  <p className="text-sm font-medium">{copy.verifiedEmail}</p>
                  <p className="text-sm text-muted-foreground">{verifiedEmail}</p>
                </div>
              <div className="flex items-center gap-2">
                <Badge
                  variant="outline"
                  className="border-emerald-500/30 bg-emerald-500/15 text-emerald-700 dark:text-emerald-300"
                  >
                    {copy.verified}
                  </Badge>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setEmail(authSettings?.email ?? "");
                    setEmailEditorOpen(true);
                  }}
                  >
                    {messages.vendorManagement.edit}
                  </Button>
              </div>
            </div>
          </div>
        ) : (
            <div className="space-y-4">
              <AuthenticationFieldShell
                label={copy.emailAddress}
                helper={copy.recoveryEmailChangedRequiresVerification}
                htmlFor="auth-email"
              >
              <Input
                id="auth-email"
                name="email"
                autoComplete="off"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                placeholder={copy.recoveryEmailPlaceholder}
                />
              </AuthenticationFieldShell>

            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => void onRequestEmailVerification()}
                disabled={sendingEmailVerification || !email.trim()}
                >
                  <MailCheck className="mr-2 h-3.5 w-3.5" />
                  {sendingEmailVerification
                    ? copy.sendingCode
                    : verificationPending
                      ? copy.resendCode
                      : copy.sendVerificationCode}
                </Button>
              </div>

            {showEmailEditor ? (
              <div className="rounded-lg border border-sky-500/25 bg-sky-500/10 p-4">
                <p className="text-sm font-medium">{copy.verifyEmail}</p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {verificationPending
                    ? copy.verificationCodeSentTo(authSettings?.pending_email ?? "")
                    : copy.verificationCodePrompt}
                </p>
                <div className="mt-3 grid gap-3 md:grid-cols-[minmax(0,1fr)_auto]">
                  <AuthenticationFieldShell label={copy.verificationCode} htmlFor="auth-email-otp">
                    <Input
                      id="auth-email-otp"
                      name="otp-code"
                      autoComplete="off"
                      value={emailVerificationOtp}
                      onChange={(event) => setEmailVerificationOtp(event.target.value)}
                      placeholder={copy.verificationOtpPlaceholder}
                    />
                  </AuthenticationFieldShell>
                  <div className="flex items-end">
                    <Button
                      type="button"
                      onClick={() => void onConfirmEmailVerification()}
                      disabled={confirmingEmailVerification || !emailVerificationOtp.trim()}
                      >
                        <MailCheck className="mr-2 h-3.5 w-3.5" />
                        {confirmingEmailVerification ? copy.verifying : copy.verify}
                      </Button>
                    </div>
                  </div>
              </div>
            ) : null}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
