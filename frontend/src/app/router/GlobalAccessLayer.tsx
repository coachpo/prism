import { useEffect, useRef } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useAuth } from "@/context/useAuth";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import { OperatorCallout, OperatorLoadingState } from "@/shared/design-system";
import { AuthPageShell } from "@/pages/AuthPageShell";

// Global access layer (SPEC §4.4/§6.3): every phase that must not render
// protected content is handled here with one unique surface per phase. The
// session-expired blocker is unclosable and traps focus until the operator
// chooses the single "重新登录" action.

export function GlobalAccessLayer() {
  const { phase, retryLogout, retryRecovery, logout } = useAuth();
  const navigate = useNavigate();
  const { messages } = useLocale();
  const blockerRef = useRef<HTMLDivElement | null>(null);

  // Trap focus inside the blocker while it is mounted.
  useEffect(() => {
    const node = blockerRef.current;
    if (!node) return;
    const previous = document.activeElement as HTMLElement | null;
    node.focus();
    return () => previous?.focus?.();
  }, []);

  const returnTo =
    phase.kind === "SESSION_EXPIRED"
      ? phase.return_to || "/observe"
      : window.location.pathname + window.location.search + window.location.hash;

  switch (phase.kind) {
    case "BOOTSTRAPPING":
      return (
        <main className="flex min-h-svh items-center justify-center bg-background px-6">
          <OperatorLoadingState className="w-full max-w-md" title={messages.auth.checkingAccess} />
        </main>
      );
    case "AUTH_DISABLED_VERIFYING":
      return (
        <AuthPageShell title={messages.auth.verifyingAccess} description={messages.auth.verifyingAccessDescription}>
          <OperatorLoadingState className="w-full max-w-md" title={messages.auth.verifyingAccess} />
        </AuthPageShell>
      );
    case "REFRESHING":
      return (
        <main className="flex min-h-svh items-center justify-center bg-background px-6">
          <OperatorLoadingState className="w-full max-w-md" title={messages.auth.refreshingSession} />
        </main>
      );
    case "LOGGING_OUT":
      return (
        <AuthPageShell
          title={phase.state === "unconfirmed" ? messages.auth.logoutUnconfirmed : messages.auth.loggingOut}
          description={phase.state === "unconfirmed" ? messages.auth.logoutUnconfirmedDescription : messages.auth.loggingOutDescription}
        >
          {phase.state === "unconfirmed" ? (
            <div className="flex flex-col items-start gap-3">
              <OperatorCallout intent="warning">{messages.auth.logoutUnconfirmedHint}</OperatorCallout>
              <Button onClick={() => void retryLogout()}>{messages.auth.retryLogout}</Button>
            </div>
          ) : (
            <OperatorLoadingState className="w-full max-w-md" title={messages.auth.loggingOut} />
          )}
        </AuthPageShell>
      );
    case "AUTH_TRANSITION_FAIL_CLOSED": {
      const title =
        phase.transition_state === "rollback_required"
          ? messages.auth.transitionRollbackRequired
          : messages.auth.transitionEnabling;
      return (
        <AuthPageShell title={title} description={messages.auth.transitionDescription}>
          <div className="flex flex-col items-start gap-3">
            <OperatorCallout intent="warning">{messages.auth.transitionHint}</OperatorCallout>
            <Button onClick={() => void retryRecovery()}>{messages.auth.refreshStatus}</Button>
          </div>
        </AuthPageShell>
      );
    }
    case "AUTH_UNAVAILABLE":
      return (
        <AuthPageShell title={messages.auth.unavailableTitle} description={messages.auth.unavailableDescription}>
          <div className="flex flex-col items-start gap-3">
            <OperatorCallout intent="danger">{messages.auth.unavailableHint}</OperatorCallout>
            <Button onClick={() => void retryRecovery()}>{messages.auth.retryAuthStatus}</Button>
          </div>
        </AuthPageShell>
      );
    case "SESSION_EXPIRED":
      return (
        <main
          ref={blockerRef}
          tabIndex={-1}
          role="alertdialog"
          aria-modal="true"
          aria-labelledby="session-expired-title"
          className="fixed inset-0 z-50 flex items-center justify-center bg-background/95 p-6"
        >
          <div className="w-full max-w-md rounded-xl border border-border bg-panel p-6 shadow-lg">
            <h1 id="session-expired-title" className="text-xl font-semibold tracking-tight">
              {messages.auth.sessionExpired}
            </h1>
            <p className="mt-2 text-sm text-muted-foreground">{messages.auth.sessionExpiredDescription}</p>
            <div className="mt-5 flex justify-end">
              <Button
                onClick={() => {
                  void logout();
                  void navigate({ to: "/auth/login", search: { redirect: returnTo } });
                }}
              >
                {messages.auth.signInAgain}
              </Button>
            </div>
          </div>
        </main>
      );
    default:
      return null;
  }
}
