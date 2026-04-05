import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { LoginPage } from "../LoginPage";

const { authState, confirmPasswordResetMock, loginMock, requestPasswordResetMock } = vi.hoisted(() => ({
  authState: {
    authEnabled: true,
    authenticated: false,
    loading: false,
  },
  confirmPasswordResetMock: vi.fn(),
  loginMock: vi.fn(),
  requestPasswordResetMock: vi.fn(),
}));

vi.mock("next-themes", () => ({
  useTheme: () => ({ resolvedTheme: "light", setTheme: vi.fn(), theme: "system" }),
}));

vi.mock("@/context/useAuth", () => ({
  useAuth: () => ({
    authEnabled: authState.authEnabled,
    authenticated: authState.authenticated,
    loading: authState.loading,
    login: loginMock,
  }),
}));

vi.mock("@/lib/api", () => ({
  api: {
    auth: {
      confirmPasswordReset: confirmPasswordResetMock,
      requestPasswordReset: requestPasswordResetMock,
    },
  },
}));

vi.mock("@/context/AuthContext", () => ({
  AuthProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@/lib/webauthn", () => ({
  authenticateWithPasskey: vi.fn(),
  isWebAuthnSupported: vi.fn(() => true),
}));

vi.mock("@/components/ui/topography", () => ({
  TopographyBackground: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

async function renderAppRoute(path: string) {
  const AppModule = await import("@/App");

  window.history.pushState({}, "", path);

  return render(
    <LocaleProvider>
      <AppModule.default />
    </LocaleProvider>,
  );
}

describe("LoginPage", () => {
  beforeEach(() => {
    localStorage.clear();
    authState.authEnabled = true;
    authState.authenticated = false;
    authState.loading = false;
    window.history.replaceState({}, "", "/login");
  });

  it("renders language controls and locale-backed auth copy", () => {
    render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/login"]}>
          <LoginPage />
        </MemoryRouter>
      </LocaleProvider>,
    );

    expect(screen.getByRole("button", { name: /^English/ })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "简体中文" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^sign in$/i })).toBeInTheDocument();
    expect(screen.getByText(/or continue with/i)).toBeInTheDocument();
  });

  it("renders login field autocomplete with autofill disabled", () => {
    render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/login"]}>
          <LoginPage />
        </MemoryRouter>
      </LocaleProvider>,
    );

    const usernameInput = screen.getByLabelText("Username");
    const passwordInput = screen.getByLabelText("Password");

    expect(usernameInput).toHaveAttribute("name", "username");
    expect(usernameInput).toHaveAttribute("autocomplete", "off");
    expect(passwordInput).toHaveAttribute("name", "password");
    expect(passwordInput).toHaveAttribute("autocomplete", "off");
  });

  it("renders forgot-password field autocomplete with autofill disabled", async () => {
    const view = await renderAppRoute("/forgot-password");
    const usernameOrEmailInput = await screen.findByLabelText(/username or email/i);

    expect(usernameOrEmailInput).toHaveAttribute("name", "username_or_email");
    expect(usernameOrEmailInput).toHaveAttribute("autocomplete", "off");

    view.unmount();
  });

  it("renders explicit reset-password field semantics on the public auth route", async () => {
    const view = await renderAppRoute("/reset-password");
    const otpCodeInput = await screen.findByLabelText(/reset code/i);
    const newPasswordInput = screen.getByLabelText(/new password/i);

    expect(otpCodeInput).toHaveAttribute("name", "otp_code");
    expect(otpCodeInput).toHaveAttribute("autocomplete", "off");
    expect(newPasswordInput).toHaveAttribute("name", "new_password");
    expect(newPasswordInput).toHaveAttribute("autocomplete", "off");

    view.unmount();
  });

  it("changes login copy when the locale changes and persists it", () => {
    render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/login"]}>
          <LoginPage />
        </MemoryRouter>
      </LocaleProvider>,
    );

    const languageTrigger = screen.getByRole("button", { name: /^English/ });
    languageTrigger.focus();
    fireEvent.keyDown(languageTrigger, { key: "Enter", code: "Enter" });
    fireEvent.click(screen.getByRole("menuitem", { name: "简体中文" }));

    expect(screen.getByRole("button", { name: "登录" })).toBeInTheDocument();
    expect(screen.getByText("或继续使用")).toBeInTheDocument();
    expect(localStorage.getItem("prism.locale")).toBe("zh-CN");
  });

  it("localizes the session duration options when switching to Chinese", () => {
    render(
      <LocaleProvider>
        <MemoryRouter initialEntries={["/login"]}>
          <LoginPage />
        </MemoryRouter>
      </LocaleProvider>,
    );

    const languageTrigger = screen.getByRole("button", { name: /^English/ });
    languageTrigger.focus();
    fireEvent.keyDown(languageTrigger, { key: "Enter", code: "Enter" });
    fireEvent.click(screen.getByRole("menuitem", { name: "简体中文" }));

    expect(screen.getAllByText("当前浏览器会话").length).toBeGreaterThan(0);
    expect(screen.getByText("7 天")).toBeInTheDocument();
    expect(screen.getByText("30 天")).toBeInTheDocument();
    expect(screen.queryByText("7 days")).not.toBeInTheDocument();
    expect(screen.queryByText("30 days")).not.toBeInTheDocument();
  });

  it("shows the locale-backed loading fallback in App route suspense", async () => {
    const view = await renderAppRoute("/login");

    expect(screen.getByText(/loading application/i)).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /^sign in$/i })).toBeInTheDocument();
    view.unmount();
  });

  it.each([
    ["/login", /^sign in$/i],
    ["/forgot-password", /send code/i],
    ["/reset-password", /reset password/i],
  ])("keeps %s on the public auth side of the route split", async (path, landmark) => {
    const view = await renderAppRoute(path);

    expect(await screen.findByRole("button", { name: landmark })).toBeInTheDocument();
    expect(window.location.pathname).toBe(path);

    view.unmount();
  });

  it("redirects unauthenticated protected routes to /login", async () => {
    const view = await renderAppRoute("/dashboard");

    expect(await screen.findByRole("button", { name: /^sign in$/i })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/login");
    expect(window.history.state?.usr?.from?.pathname).toBe("/dashboard");

    view.unmount();
  });
});
