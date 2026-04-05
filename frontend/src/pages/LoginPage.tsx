import { useState } from "react";
import { useTheme } from "next-themes";
import { Fingerprint } from "lucide-react";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";

import { GlobalPreferencesControls } from "@/components/GlobalPreferencesControls";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { TopographyBackground } from "@/components/ui/topography";
import { useAuth } from "@/context/useAuth";
import { useLocale } from "@/i18n/useLocale";
import type { LoginSessionDuration } from "@/lib/types";
import { authenticateWithPasskey, isWebAuthnSupported } from "@/lib/webauthn";

export function LoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const { resolvedTheme } = useTheme();
  const { authEnabled, authenticated, loading, login } = useAuth();
  const { locale, messages } = useLocale();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [sessionDuration, setSessionDuration] = useState<LoginSessionDuration>("session");
  const [submitting, setSubmitting] = useState(false);
  const [passkeyLoading, setPasskeyLoading] = useState(false);

  if (!loading && !authEnabled) {
    return <Navigate to="/dashboard" replace />;
  }

  if (!loading && authenticated) {
    const fromLocation = (location.state as {
      from?: { pathname?: string; search?: string; hash?: string };
    } | null)?.from;
    const nextPath = fromLocation
      ? `${fromLocation.pathname ?? ""}${fromLocation.search ?? ""}${fromLocation.hash ?? ""}`
      : null;
    return <Navigate to={nextPath || "/dashboard"} replace />;
  }

  const handleSubmit = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      const fromLocation = (location.state as {
        from?: { pathname?: string; search?: string; hash?: string };
      } | null)?.from;
      const nextPath = fromLocation
        ? `${fromLocation.pathname ?? ""}${fromLocation.search ?? ""}${fromLocation.hash ?? ""}`
        : null;

      await login(username.trim(), password, sessionDuration);
      navigate(nextPath || "/dashboard", { replace: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.auth.loginFailed);
    } finally {
      setSubmitting(false);
    }
  };

  const handlePasskeyLogin = async () => {
    if (!isWebAuthnSupported()) {
      toast.error(messages.auth.browserNoPasskeys);
      return;
    }

    setPasskeyLoading(true);
    try {
      const fromLocation = (location.state as {
        from?: { pathname?: string; search?: string; hash?: string };
      } | null)?.from;
      const nextPath = fromLocation
        ? `${fromLocation.pathname ?? ""}${fromLocation.search ?? ""}${fromLocation.hash ?? ""}`
        : null;
      const result = await authenticateWithPasskey(username.trim() || undefined);

      if (result.success && result.authenticated) {
        window.location.assign(nextPath || "/dashboard");
      } else {
        toast.error(messages.auth.passkeyAuthenticationFailed);
      }
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : messages.auth.passkeyAuthenticationFailed
      );
    } finally {
      setPasskeyLoading(false);
    }
  };

  const isDarkTheme = resolvedTheme === "dark";
  const topographyBackgroundColor = isDarkTheme ? "#091120" : "#f8fbff";
  const topographyLineColor = isDarkTheme
    ? "rgba(148, 163, 184, 0.18)"
    : "rgba(71, 85, 105, 0.12)";

  return (
    <TopographyBackground
      backgroundColor={topographyBackgroundColor}
      lineColor={topographyLineColor}
      lineCount={18}
      speed={0.35}
      strokeWidth={1.1}
    >
      <div className="relative min-h-screen overflow-hidden text-foreground">
        <div className="pointer-events-none absolute inset-0">
          <div className="absolute inset-0 bg-[radial-gradient(circle_at_top_left,rgba(59,130,246,0.18),transparent_36%),radial-gradient(circle_at_bottom_right,rgba(14,165,233,0.12),transparent_34%)] dark:bg-[radial-gradient(circle_at_top_left,rgba(96,165,250,0.18),transparent_34%),radial-gradient(circle_at_bottom_right,rgba(56,189,248,0.14),transparent_36%)]" />
          <div className="absolute left-[8%] top-20 h-48 w-48 rounded-full bg-primary/12 blur-3xl dark:bg-primary/18" />
          <div className="absolute bottom-16 right-[10%] h-64 w-64 rounded-full bg-sky-500/10 blur-3xl dark:bg-cyan-400/12" />
        </div>

        <div className="relative flex min-h-screen flex-col">
          <div className="flex items-center justify-between px-4 pt-4 sm:px-6 sm:pt-6 lg:px-8">
            <div />

            <GlobalPreferencesControls
              languageSwitcherClassName="border-border/70 bg-background/70 shadow-sm backdrop-blur-xl"
              themeToggleButtonClassName="h-9 w-9 rounded-full border border-border/70 bg-background/70 text-foreground shadow-sm backdrop-blur-xl hover:bg-background/90"
              themeToggleMenuClassName="border-border/70 bg-popover/95 backdrop-blur-xl"
            />
          </div>

          <div className="mx-auto flex w-full max-w-6xl flex-1 items-center justify-center px-4 pb-8 pt-6 sm:px-6 sm:pb-10 lg:px-8">
            <Card className="w-full max-w-[420px] border-border/70 bg-background/78 shadow-2xl shadow-primary/10 backdrop-blur-2xl">
              <CardHeader className="pb-6">
                <CardTitle className="text-2xl tracking-tight">{messages.auth.signIn}</CardTitle>
                <CardDescription className="sr-only">
                  {messages.auth.signInDescription}
                </CardDescription>
              </CardHeader>

              <CardContent className="space-y-6">
                <form className="space-y-5" onSubmit={handleSubmit}>
                  <div className="space-y-2">
                    <Label htmlFor="username">{messages.auth.username}</Label>
                    <Input
                      id="username"
                      name="username"
                      autoComplete="off"
                      value={username}
                      onChange={(event) => setUsername(event.target.value)}
                      className="bg-background/80"
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="password">{messages.auth.password}</Label>
                    <Input
                      id="password"
                      name="password"
                      type="password"
                      autoComplete="off"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      className="bg-background/80"
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="session-duration">{messages.auth.keepSignedInFor}</Label>
                    <Select
                      key={locale}
                      value={sessionDuration}
                      onValueChange={(value: LoginSessionDuration) => setSessionDuration(value)}
                    >
                      <SelectTrigger id="session-duration" className="bg-background/80">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="session">{messages.auth.sessionCurrent}</SelectItem>
                        <SelectItem value="7_days">{messages.auth.session7Days}</SelectItem>
                        <SelectItem value="30_days">{messages.auth.session30Days}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="flex flex-col-reverse gap-3 sm:flex-row sm:items-center sm:justify-between">
                    <Button
                      type="button"
                      variant="link"
                      className="justify-start px-0 text-muted-foreground"
                      onClick={() => navigate("/forgot-password")}
                    >
                      {messages.auth.forgotPasswordQuestion}
                    </Button>
                    <Button
                      type="submit"
                      className="min-w-28"
                      disabled={submitting || loading || passkeyLoading}
                    >
                      {submitting ? messages.auth.signingIn : messages.auth.signIn}
                    </Button>
                  </div>
                </form>

                <div className="relative">
                  <div className="absolute inset-0 flex items-center">
                    <span className="w-full border-t border-border/70" />
                  </div>
                  <div className="relative flex justify-center text-xs uppercase tracking-[0.24em]">
                    <span className="bg-card/90 px-3 text-muted-foreground backdrop-blur-sm">
                      {messages.auth.orContinueWith}
                    </span>
                  </div>
                </div>

                <Button
                  type="button"
                  variant="outline"
                  className="h-11 w-full justify-center gap-2 border-border/70 bg-background/70 hover:bg-background/90"
                  onClick={handlePasskeyLogin}
                  disabled={submitting || loading || passkeyLoading}
                >
                  <Fingerprint className="h-4 w-4" />
                  {passkeyLoading ? messages.auth.authenticating : messages.auth.signInWithPasskey}
                </Button>
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </TopographyBackground>
  );
}
