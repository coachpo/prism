import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { OperatorEmailCard } from "../sections/authentication/OperatorEmailCard";
import { RecoveryEmailCard } from "../sections/authentication/RecoveryEmailCard";

describe("authentication setup cards", () => {
  it("renders the operator account card with save-state action", () => {
    render(
      <LocaleProvider>
        <OperatorEmailCard
          authEnabled={false}
          authSaving={false}
          authSettings={{
            auth_enabled: false,
            email: null,
            email_bound_at: null,
            email_verification_required: true,
            has_password: false,
            pending_email: null,
            proxy_key_limit: 100,
            username: null,
          }}
          onSaveAuthSettings={vi.fn()}
          password=""
          passwordConfirm=""
          passwordError={null}
          passwordMismatch={false}
          setPassword={vi.fn()}
          setPasswordConfirm={vi.fn()}
          setUsername={vi.fn()}
          username="admin"
        />
      </LocaleProvider>,
    );

    const usernameInput = screen.getByLabelText("Username");
    const passwordInput = screen.getByLabelText("Password");
    const confirmPasswordInput = screen.getByLabelText("Confirm password");

    expect(screen.getByText("Operator account")).toBeInTheDocument();
    expect(usernameInput).toHaveAttribute("name", "username");
    expect(usernameInput).toHaveAttribute("autocomplete", "off");
    expect(passwordInput).toHaveAttribute("name", "password");
    expect(passwordInput).toHaveAttribute("autocomplete", "off");
    expect(confirmPasswordInput).toHaveAttribute("name", "password-confirm");
    expect(confirmPasswordInput).toHaveAttribute("autocomplete", "off");
    expect(screen.getByRole("button", { name: "Save account changes" })).toBeInTheDocument();
  });

  it("renders the recovery email card verification controls", () => {
    render(
      <LocaleProvider>
        <RecoveryEmailCard
          authSettings={{
            auth_enabled: false,
            email: null,
            email_bound_at: null,
            email_verification_required: true,
            has_password: true,
            pending_email: null,
            proxy_key_limit: 100,
            username: "admin",
          }}
          confirmingEmailVerification={false}
          email="operator@example.com"
          emailVerificationOtp=""
          onConfirmEmailVerification={vi.fn()}
          onRequestEmailVerification={vi.fn()}
          sendingEmailVerification={false}
          setEmail={vi.fn()}
          setEmailVerificationOtp={vi.fn()}
        />
      </LocaleProvider>,
    );

    const recoveryEmailInput = screen.getByLabelText("Email address");
    const verificationCodeInput = screen.getByLabelText("Verification code");

    expect(screen.getByText("Recovery email")).toBeInTheDocument();
    expect(recoveryEmailInput).toHaveAttribute("name", "email");
    expect(recoveryEmailInput).toHaveAttribute("autocomplete", "off");
    expect(verificationCodeInput).toHaveAttribute("id", "auth-email-otp");
    expect(verificationCodeInput).toHaveAttribute("name", "otp-code");
    expect(verificationCodeInput).toHaveAttribute("autocomplete", "off");
    expect(screen.getByRole("button", { name: "Send verification code" })).toBeInTheDocument();
  });

  it("renders localized auth setup card copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    render(
      <LocaleProvider>
        <>
          <OperatorEmailCard
            authEnabled={false}
            authSaving={false}
            authSettings={{
              auth_enabled: false,
              email: null,
              email_bound_at: null,
              email_verification_required: true,
              has_password: false,
              pending_email: null,
              proxy_key_limit: 100,
              username: null,
            }}
            onSaveAuthSettings={vi.fn()}
            password=""
            passwordConfirm=""
            passwordError={null}
            passwordMismatch={false}
            setPassword={vi.fn()}
            setPasswordConfirm={vi.fn()}
            setUsername={vi.fn()}
            username="admin"
          />
          <RecoveryEmailCard
            authSettings={{
              auth_enabled: false,
              email: null,
              email_bound_at: null,
              email_verification_required: true,
              has_password: true,
              pending_email: null,
              proxy_key_limit: 100,
              username: "admin",
            }}
            confirmingEmailVerification={false}
            email="operator@example.com"
            emailVerificationOtp=""
            onConfirmEmailVerification={vi.fn()}
            onRequestEmailVerification={vi.fn()}
            sendingEmailVerification={false}
            setEmail={vi.fn()}
            setEmailVerificationOtp={vi.fn()}
          />
        </>
      </LocaleProvider>,
    );

    expect(screen.getByText("操作员账户")).toBeInTheDocument();
    expect(screen.getByText("恢复邮箱")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存账户更改" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "发送验证码" })).toBeInTheDocument();
  });
});
