import { useState, type ComponentProps } from "react";
import { useTheme } from "next-themes";
import { Navigate, useLocation, useNavigate } from "react-router-dom";
import { toast } from "sonner";

import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { TopographyBackground } from "@/components/ui/topography";
import { useAuth } from "@/context/useAuth";
import { useLocale } from "@/i18n/useLocale";
import type { LoginSessionDuration } from "@/lib/types";

type LoginFormSubmitEvent = Parameters<NonNullable<ComponentProps<"form">["onSubmit"]>>[0];

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

  const handleSubmit = async (event: LoginFormSubmitEvent) => {
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
            <div className="rounded-full border border-border/60 bg-background/70 px-3 py-1 text-xs font-medium tracking-tight shadow-sm backdrop-blur-xl">
              Prism
            </div>

            <div className="flex items-center gap-2">
              <LanguageSwitcher
                buttonClassName="border-border/70 bg-background/70 shadow-sm backdrop-blur-xl"
                menuClassName="border-border/70 bg-popover/95 backdrop-blur-xl"
              />
              <ThemeToggle
                buttonClassName="h-9 w-9 rounded-full border border-border/70 bg-background/70 text-foreground shadow-sm backdrop-blur-xl hover:bg-background/90"
                menuClassName="border-border/70 bg-popover/95 backdrop-blur-xl"
              />
            </div>
          </div>

          <div className="mx-auto flex w-full max-w-6xl flex-1 items-center justify-center px-4 pb-8 pt-6 sm:px-6 sm:pb-10 lg:px-8">
            <Card className="w-full max-w-[440px] overflow-hidden border-border/70 bg-card/90 shadow-2xl shadow-primary/10 backdrop-blur-2xl">
              <CardHeader className="gap-2 border-b border-border/60 pb-5">
                <CardTitle className="text-2xl font-semibold tracking-tight sm:text-3xl">{messages.auth.signIn}</CardTitle>
                <CardDescription className="max-w-sm text-sm leading-6">
                  {messages.auth.signInDescription}
                </CardDescription>
              </CardHeader>

              <CardContent>
                <form className="flex flex-col gap-5" onSubmit={handleSubmit}>
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="username">{messages.auth.username}</Label>
                    <Input
                      id="username"
                      name="username"
                      autoComplete="username"
                      value={username}
                      onChange={(event) => setUsername(event.target.value)}
                      className="bg-background/90"
                    />
                  </div>

                  <div className="flex flex-col gap-2">
                    <Label htmlFor="password">{messages.auth.password}</Label>
                    <Input
                      id="password"
                      name="password"
                      type="password"
                      autoComplete="current-password"
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      className="bg-background/90"
                    />
                  </div>

                  <div className="flex flex-col gap-2">
                    <Label htmlFor="session-duration">{messages.auth.keepSignedInFor}</Label>
                    <Select
                      key={locale}
                      value={sessionDuration}
                      onValueChange={(value: LoginSessionDuration) => setSessionDuration(value)}
                    >
                      <SelectTrigger id="session-duration" className="bg-background/90">
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
                      className="justify-start px-0 text-muted-foreground hover:text-foreground"
                      onClick={() => navigate("/forgot-password")}
                    >
                      {messages.auth.forgotPasswordQuestion}
                    </Button>
                    <Button
                      type="submit"
                      className="min-w-28"
                      disabled={submitting || loading}
                    >
                      {submitting ? messages.auth.signingIn : messages.auth.signIn}
                    </Button>
                  </div>
                </form>
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </TopographyBackground>
  );
}
