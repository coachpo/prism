import { useState, type ComponentProps } from "react";
import { Navigate, useNavigate, useSearch } from "@tanstack/react-router";
import { toast } from "sonner";

import { AuthPageShell } from "@/pages/AuthPageShell";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { useAuth } from "@/context/useAuth";
import { useLocale } from "@/i18n/useLocale";
import type { LoginSessionDuration } from "@/lib/types";

type LoginFormSubmitEvent = Parameters<NonNullable<ComponentProps<"form">["onSubmit"]>>[0];

function resolveLoginRedirect(redirect: string | undefined): string {
  if (redirect?.startsWith("/") && !redirect.startsWith("//")) {
    return redirect;
  }
  return "/observe";
}

export function LoginPage() {
  const navigate = useNavigate();
  const search = useSearch({ from: "/auth/login" });
  const { authEnabled, authenticated, loading, login } = useAuth();
  const { locale, messages } = useLocale();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [sessionDuration, setSessionDuration] = useState<LoginSessionDuration>("session");
  const [submitting, setSubmitting] = useState(false);

  if (!loading && !authEnabled) {
    return <Navigate to="/observe" replace />;
  }

  if (!loading && authenticated) {
    return <Navigate to={resolveLoginRedirect(search.redirect)} replace />;
  }

  const handleSubmit = async (event: LoginFormSubmitEvent) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      await login(username.trim(), password, sessionDuration);
      await navigate({ to: resolveLoginRedirect(search.redirect), replace: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.auth.loginFailed);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <AuthPageShell
      title={messages.auth.signIn}
      description={messages.auth.signInDescription}
    >
      <form onSubmit={handleSubmit}>
        <FieldGroup className="gap-5">
          <Field>
            <FieldLabel htmlFor="username">{messages.auth.username}</FieldLabel>
            <Input
              id="username"
              name="username"
              autoComplete="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="password">{messages.auth.password}</FieldLabel>
            <Input
              id="password"
              name="password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="session-duration">{messages.auth.keepSignedInFor}</FieldLabel>
            <Select
              key={locale}
              value={sessionDuration}
              onValueChange={(value: LoginSessionDuration) => setSessionDuration(value)}
            >
              <SelectTrigger id="session-duration">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="session">{messages.auth.sessionCurrent}</SelectItem>
                  <SelectItem value="7_days">{messages.auth.session7Days}</SelectItem>
                  <SelectItem value="30_days">{messages.auth.session30Days}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>

          <Button
            type="submit"
            className="min-w-28 self-end"
            disabled={submitting || loading}
          >
            {submitting ? <Spinner data-icon="inline-start" /> : null}
            {submitting ? messages.auth.signingIn : messages.auth.signIn}
          </Button>
        </FieldGroup>
      </form>
    </AuthPageShell>
  );
}
