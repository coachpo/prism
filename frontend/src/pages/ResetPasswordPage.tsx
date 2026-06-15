import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";

import { api } from "@/lib/api";
import { AuthPageShell } from "@/pages/AuthPageShell";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useLocale } from "@/i18n/useLocale";

export function ResetPasswordPage() {
  const navigate = useNavigate();
  const { messages } = useLocale();
  const [otpCode, setOtpCode] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      await api.auth.confirmPasswordReset({ otp_code: otpCode.trim(), new_password: newPassword });
      toast.success(messages.auth.passwordUpdated);
      navigate("/login", { replace: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.auth.resetPasswordError);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AuthPageShell
      title={messages.auth.enterResetCode}
      description={messages.auth.resetPasswordDescription}
    >
      <form onSubmit={handleSubmit}>
        <FieldGroup className="gap-5">
          <Field>
            <FieldLabel htmlFor="otp-code">{messages.auth.resetCode}</FieldLabel>
            <Input
              id="otp-code"
              name="otp_code"
              autoComplete="off"
              value={otpCode}
              onChange={(event) => setOtpCode(event.target.value)}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="new-password">{messages.auth.newPassword}</FieldLabel>
            <Input
              id="new-password"
              name="new_password"
              type="password"
              autoComplete="off"
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
            />
          </Field>
          <div className="flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
            <Button type="button" variant="link" className="justify-start px-0" onClick={() => navigate("/login")}>
              {messages.auth.backToLogin}
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? <Spinner data-icon="inline-start" /> : null}
              {submitting ? messages.auth.resetting : messages.auth.resetPassword}
            </Button>
          </div>
        </FieldGroup>
      </form>
    </AuthPageShell>
  );
}
