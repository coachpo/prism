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

export function ForgotPasswordPage() {
  const navigate = useNavigate();
  const { messages } = useLocale();
  const [usernameOrEmail, setUsernameOrEmail] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      await api.auth.requestPasswordReset({ username_or_email: usernameOrEmail.trim() });
      toast.success(messages.auth.accountResetCodeSent);
      navigate("/reset-password", { replace: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.auth.forgotPasswordError);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AuthPageShell
      title={messages.auth.resetPasswordTitle}
      description={messages.auth.forgotPasswordDescription}
    >
      <form onSubmit={handleSubmit}>
        <FieldGroup className="gap-5">
          <Field>
            <FieldLabel htmlFor="username-or-email">{messages.auth.usernameOrEmail}</FieldLabel>
            <Input
              id="username-or-email"
              name="username_or_email"
              autoComplete="off"
              value={usernameOrEmail}
              onChange={(event) => setUsernameOrEmail(event.target.value)}
            />
          </Field>
          <div className="flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
            <Button type="button" variant="link" className="justify-start px-0" onClick={() => navigate("/login")}>
              {messages.auth.backToLogin}
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? <Spinner data-icon="inline-start" /> : null}
              {submitting ? messages.auth.sending : messages.auth.sendCode}
            </Button>
          </div>
        </FieldGroup>
      </form>
    </AuthPageShell>
  );
}
