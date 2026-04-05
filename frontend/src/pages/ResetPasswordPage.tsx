import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";

import { api } from "@/lib/api";
import { GlobalPreferencesControls } from "@/components/GlobalPreferencesControls";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
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
    <div className="min-h-screen px-4 py-4 sm:px-6 sm:py-6">
      <div className="mx-auto flex max-w-6xl justify-end pb-6">
        <GlobalPreferencesControls
          languageSwitcherClassName="border-border/70 bg-background/70 shadow-sm backdrop-blur-xl"
          themeToggleButtonClassName="h-9 w-9 rounded-full border border-border/70 bg-background/70 text-foreground shadow-sm backdrop-blur-xl hover:bg-background/90"
          themeToggleMenuClassName="border-border/70 bg-popover/95 backdrop-blur-xl"
        />
      </div>
      <div className="mx-auto flex max-w-md items-center justify-center">
        <Card className="w-full">
          <CardHeader>
            <CardTitle>{messages.auth.enterResetCode}</CardTitle>
            <CardDescription>{messages.auth.resetPasswordDescription}</CardDescription>
          </CardHeader>
          <CardContent>
            <form className="space-y-4" onSubmit={handleSubmit}>
              <div className="space-y-2">
                <Label htmlFor="otp-code">{messages.auth.resetCode}</Label>
                <Input
                  id="otp-code"
                  name="otp_code"
                  autoComplete="off"
                  value={otpCode}
                  onChange={(event) => setOtpCode(event.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="new-password">{messages.auth.newPassword}</Label>
                <Input
                  id="new-password"
                  name="new_password"
                  type="password"
                  autoComplete="off"
                  value={newPassword}
                  onChange={(event) => setNewPassword(event.target.value)}
                />
              </div>
              <div className="flex items-center justify-between gap-3">
                <Button type="button" variant="link" className="px-0" onClick={() => navigate("/login")}>{messages.auth.backToLogin}</Button>
                <Button type="submit" disabled={submitting}>
                  {submitting ? messages.auth.resetting : messages.auth.resetPassword}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

