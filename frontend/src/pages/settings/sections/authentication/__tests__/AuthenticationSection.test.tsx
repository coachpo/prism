import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { AuthenticationSection } from "../../AuthenticationSection";

vi.mock("../authentication/PasskeysCard", () => ({
  PasskeysCard: () => <div>passkeys</div>,
}));

describe("AuthenticationSection i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized status copy when authentication is disabled", () => {
    render(
      <LocaleProvider>
        <AuthenticationSection
          authEnabled={false}
          authSettings={{
            auth_enabled: false,
            username: null,
            email: null,
            email_bound_at: null,
            pending_email: null,
            email_verification_required: true,
            has_password: false,
            proxy_key_limit: 100,
          }}
          authSaving={false}
          username="admin"
          setUsername={vi.fn()}
          email=""
          setEmail={vi.fn()}
          password=""
          passwordError={null}
          setPassword={vi.fn()}
          passwordConfirm=""
          passwordMismatch={false}
          setPasswordConfirm={vi.fn()}
          emailVerificationOtp=""
          setEmailVerificationOtp={vi.fn()}
          sendingEmailVerification={false}
          confirmingEmailVerification={false}
          onRequestEmailVerification={vi.fn()}
          onConfirmEmailVerification={vi.fn()}
          onSaveAuthSettings={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getAllByText("身份验证").length).toBeGreaterThan(0);
    expect(screen.getByText("已禁用")).toBeInTheDocument();
  });
});
