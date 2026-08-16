import { useEffect, useRef, useState, type ComponentProps } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { Eye, EyeOff } from "lucide-react";

import { AuthPageShell } from "@/pages/AuthPageShell";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { OperatorCallout, OperatorMissingValue } from "@/shared/design-system";
import { useAuth } from "@/context/useAuth";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { api, ApiError } from "@/lib/api";
import { isSafeReturnPath } from "@/app/router/authGates";
import type { AuthLoginLockedDetails, LoginSessionDuration, PublicAuthStatus } from "@/lib/types";

type LoginFormSubmitEvent = Parameters<NonNullable<ComponentProps<"form">["onSubmit"]>>[0];

type LoginStatus =
  | { kind: "idle" }
  | { kind: "submitting" }
  | { kind: "invalid_credentials" }
  | { kind: "locked"; retryAt: number }
  | { kind: "error"; message: string };

function resolveLoginRedirect(redirect: string | undefined): string {
  if (redirect && isSafeReturnPath(redirect)) {
    return redirect;
  }
  return "/observe";
}

function formatCountdown(remainingSeconds: number): string {
  const hours = Math.floor(remainingSeconds / 3600);
  const minutes = Math.floor((remainingSeconds % 3600) / 60);
  const seconds = remainingSeconds % 60;
  // A 90 minute lock must read as 01:30:00, never as 90:00.
  return [hours, minutes, seconds].map((part) => String(part).padStart(2, "0")).join(":");
}

export function LoginPage() {
  const navigate = useNavigate();
  const search = useSearch({ from: "/auth/login" });
  const { authEnabled, authenticated, loading, login } = useAuth();
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [passwordVisible, setPasswordVisible] = useState(false);
  const [capsLockOn, setCapsLockOn] = useState(false);
  const [sessionDuration, setSessionDuration] = useState<LoginSessionDuration>("session");
  const [status, setStatus] = useState<LoginStatus>({ kind: "idle" });
  const [publicStatus, setPublicStatus] = useState<PublicAuthStatus | null>(null);
  const [publicStatusFailed, setPublicStatusFailed] = useState(false);
  const passwordRef = useRef<HTMLInputElement | null>(null);

  const redirect = resolveLoginRedirect(search.redirect);
  // `redirect` is only present when something bounced the operator here, which
  // is exactly the case that needs explaining.
  const returnedFromProtectedPage = Boolean(search.redirect);

  // The public status endpoint is what the login gate itself keys on, so the
  // page states it rather than leaving a failed sign-in unexplained.
  useEffect(() => {
    let cancelled = false;
    void api.auth
      .status()
      .then((value) => {
        if (!cancelled) setPublicStatus(value);
      })
      .catch(() => {
        if (!cancelled) setPublicStatusFailed(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Countdown for the locked state: the server retry_at is authoritative;
  // local state may be lost on reload and the next submit recalibrates.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (status.kind !== "locked") {
      return;
    }
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [status.kind]);

  const lockedRemainingSeconds =
    status.kind === "locked" ? Math.max(0, Math.ceil((status.retryAt - now) / 1000)) : 0;

  // Countdown reaching zero re-enables submit (the backend stays
  // authoritative).
  useEffect(() => {
    if (status.kind === "locked" && lockedRemainingSeconds === 0) {
      setStatus({ kind: "idle" });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lockedRemainingSeconds]);

  const instanceFacts = publicStatusFailed ? (
    <span>{messages.auth.statusUnavailable}</span>
  ) : publicStatus ? (
    <>
      <span>
        {messages.auth.instanceStatusLabel}: {authStateLabel(publicStatus.state, messages)}
      </span>
      <span>
        {messages.auth.generationLabel}: {publicStatus.effective_generation}
      </span>
      <span>
        {messages.auth.loginAvailableLabel}:{" "}
        {publicStatus.login_available ? messages.auth.loginAvailableYes : messages.auth.loginAvailableNo}
      </span>
    </>
  ) : (
    <OperatorMissingValue reason={messages.auth.checkingAccess} />
  );

  // Auth disabled: render the open-access explainer, never the form.
  if (!loading && !authEnabled) {
    return (
      <AuthPageShell
        title={messages.auth.disabledTitle}
        description={messages.auth.disabledDescription}
        facts={instanceFacts}
      >
        <div className="flex flex-col items-start gap-4">
          <OperatorCallout intent="warning">{messages.auth.disabledWarning}</OperatorCallout>
          <details className="rounded-md border border-border bg-inset px-3 py-2 text-sm">
            <summary className="cursor-pointer font-medium text-foreground">{messages.common.moreDetails}</summary>
            <p className="pt-2 text-muted-foreground">{messages.auth.disabledWarningDetails}</p>
          </details>
          <p className="text-sm text-muted-foreground">{messages.auth.disabledProxyKeyNote}</p>
          <div className="flex flex-wrap gap-3">
            <Button onClick={() => void navigate({ to: "/observe" })}>{messages.auth.enterConsole}</Button>
            <Button variant="outline" onClick={() => void navigate({ to: "/system/settings?scope=instance&section=authentication" })}>
              {messages.auth.goToAuthSettings}
            </Button>
          </div>
        </div>
      </AuthPageShell>
    );
  }

  // Authenticated on the login page: continue to the safe target.
  if (!loading && authenticated) {
    return <NavigateTo to={redirect} />;
  }

  const handleSubmit = async (event: LoginFormSubmitEvent) => {
    event.preventDefault();
    if (status.kind === "submitting" || status.kind === "locked") {
      return;
    }
    setStatus({ kind: "submitting" });
    try {
      await login(username.trim(), password, sessionDuration);
      await navigate({ to: redirect, replace: true });
    } catch (error) {
      if (error instanceof ApiError) {
        if (error.status === 401 && error.code === "auth_invalid_credentials") {
          setPassword("");
          setStatus({ kind: "invalid_credentials" });
          passwordRef.current?.focus();
          return;
        }
        if (error.status === 429 && error.code === "auth_login_locked") {
          const details = error.details as AuthLoginLockedDetails | undefined;
          const retryAt = details?.retry_at ? Date.parse(details.retry_at) : Number.NaN;
          if (Number.isFinite(retryAt)) {
            setPassword("");
            setStatus({ kind: "locked", retryAt });
            return;
          }
          setPassword("");
          setStatus({ kind: "error", message: messages.auth.lockedFallback });
          return;
        }
        if (error.status === 400 && error.code === "auth_not_enabled") {
          setPassword("");
          await navigate({ to: "/auth/login", replace: true });
          window.location.reload();
          return;
        }
      }
      setStatus({ kind: "error", message: messages.auth.loginTemporarilyUnavailable });
    }
  };

  const locked = status.kind === "locked";

  const banner = returnedFromProtectedPage ? (
    <div data-testid="login-return-banner" className="flex flex-col gap-0.5">
      <p className="text-[0.8125rem] font-medium text-foreground">{messages.auth.signInToContinue}</p>
      <p className="font-mono text-[11px] text-muted-foreground">{messages.auth.returningTo(redirect)}</p>
    </div>
  ) : null;

  // Locked is a card-level state, not another notice stacked above the form.
  if (locked) {
    return (
      <AuthPageShell
        title={messages.auth.lockedTitle}
        description={messages.auth.lockedDescription}
        banner={banner}
        facts={instanceFacts}
      >
        <div className="flex flex-col items-center gap-4 py-2" role="alert" aria-live="polite">
          <p className="text-xs text-muted-foreground">{messages.auth.lockedRetryIn}</p>
          <p data-testid="login-locked-countdown" className="font-mono text-4xl font-semibold tabular-nums">
            {formatCountdown(lockedRemainingSeconds)}
          </p>
          <p className="text-xs text-muted-foreground">
            {messages.auth.lockedRetryAt(formatTime(new Date(status.retryAt).toISOString()))}
          </p>
          {username.trim() ? (
            <div className="w-full rounded-md border border-border bg-inset px-3 py-2">
              <p className="text-[11px] font-medium tracking-[0.04em] text-muted-foreground">
                {messages.auth.lockedSummaryLabel}
              </p>
              <p className="truncate font-mono text-[0.8125rem]">{username.trim()}</p>
            </div>
          ) : null}
          <Button type="button" className="w-full" disabled>
            {messages.auth.lockedButton(formatCountdown(lockedRemainingSeconds))}
          </Button>
        </div>
      </AuthPageShell>
    );
  }

  return (
    <AuthPageShell
      title={messages.auth.signIn}
      description={messages.auth.signInDescription}
      banner={banner}
      facts={instanceFacts}
    >
      <form onSubmit={handleSubmit} noValidate={false}>
        <FieldGroup className="gap-4">
          {/* One status slot: every failure mode renders here at the same
              visual level, and the page never also raises a toast. */}
          {status.kind === "invalid_credentials" ? (
            <OperatorCallout intent="danger" role="alert" data-testid="login-status">
              {messages.auth.invalidCredentials}
            </OperatorCallout>
          ) : null}
          {status.kind === "error" ? (
            <OperatorCallout intent="danger" role="alert" data-testid="login-status">
              {status.message}
            </OperatorCallout>
          ) : null}

          <Field>
            <FieldLabel htmlFor="username">{messages.auth.username}</FieldLabel>
            <Input
              id="username"
              name="username"
              autoComplete="username"
              aria-invalid={status.kind === "invalid_credentials" || undefined}
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="password">{messages.auth.password}</FieldLabel>
            <div className="relative">
              <Input
                ref={passwordRef}
                id="password"
                name="password"
                type={passwordVisible ? "text" : "password"}
                autoComplete="current-password"
                aria-invalid={status.kind === "invalid_credentials" || undefined}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                onKeyUp={(event) => setCapsLockOn(event.getModifierState("CapsLock"))}
                onKeyDown={(event) => setCapsLockOn(event.getModifierState("CapsLock"))}
                className="pr-10"
              />
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => setPasswordVisible((visible) => !visible)}
                aria-label={passwordVisible ? messages.auth.hidePassword : messages.auth.showPassword}
                aria-pressed={passwordVisible}
                className="absolute right-1 top-1/2 -translate-y-1/2 text-muted-foreground"
              >
                {passwordVisible ? <EyeOff /> : <Eye />}
              </Button>
            </div>
            {capsLockOn ? (
              <FieldDescription className="text-degraded" data-testid="login-caps-lock">
                {messages.auth.capsLockOn}
              </FieldDescription>
            ) : (
              <FieldDescription>{messages.auth.passwordClearedHint}</FieldDescription>
            )}
          </Field>

          <Field>
            <FieldLabel htmlFor="session-duration">{messages.auth.keepSignedInFor}</FieldLabel>
            <Select
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
            <FieldDescription>{messages.auth.keepSignedInDescription}</FieldDescription>
          </Field>

          <Button
            type="submit"
            className="w-full"
            disabled={status.kind === "submitting" || loading}
            aria-busy={status.kind === "submitting" || undefined}
          >
            {status.kind === "submitting" ? <Spinner data-icon="inline-start" /> : null}
            {status.kind === "submitting" ? messages.auth.signingIn : messages.auth.signIn}
          </Button>
        </FieldGroup>
      </form>
    </AuthPageShell>
  );
}

function authStateLabel(state: PublicAuthStatus["state"], messages: ReturnType<typeof useLocale>["messages"]): string {
  switch (state) {
    case "enabled":
      return messages.auth.stateEnabled;
    case "disabled":
      return messages.auth.stateDisabled;
    default:
      return messages.auth.stateTransitionFailClosed;
  }
}

function NavigateTo({ to }: { to: string }) {
  const navigate = useNavigate();
  useEffect(() => {
    void navigate({ to, replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [to]);
  return null;
}
