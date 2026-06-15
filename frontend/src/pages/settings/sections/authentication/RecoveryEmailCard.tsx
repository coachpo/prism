import { useState } from "react";
import { MailCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { Input } from "@/components/ui/input";
import { OperatorCallout, OperatorStatusBadge } from "@/shared/design-system";
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
          <OperatorCallout
            action={(
              <div className="flex items-center gap-2">
                <OperatorStatusBadge label={copy.verified} intent="success" />
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => {
                    setEmail(authSettings?.email ?? "");
                    setEmailEditorOpen(true);
                  }}
                >
                  {messages.common.edit}
                </Button>
              </div>
            )}
            description={verifiedEmail}
            intent="success"
            title={copy.verifiedEmail}
          />
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
              <OperatorCallout intent="info" title={copy.verifyEmail}>
                <div className="flex flex-col gap-3">
                  <p className="text-xs">
                    {verificationPending
                      ? copy.verificationCodeSentTo(authSettings?.pending_email ?? "")
                      : copy.verificationCodePrompt}
                  </p>
                  <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto]">
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
              </OperatorCallout>
            ) : null}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
