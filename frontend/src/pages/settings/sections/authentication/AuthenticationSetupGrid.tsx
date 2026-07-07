import type { AuthenticationSectionProps } from "./types";
import { OperatorEmailCard } from "./OperatorEmailCard";

export function AuthenticationSetupGrid({
  authEnabled,
  authSaving,
  authSettings,
  username,
  setUsername,
  password,
  passwordError,
  setPassword,
  passwordConfirm,
  passwordMismatch,
  setPasswordConfirm,
  onSaveAuthSettings,
}: AuthenticationSectionProps) {
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <OperatorEmailCard
        authEnabled={authEnabled}
        authSaving={authSaving}
        authSettings={authSettings}
        onSaveAuthSettings={onSaveAuthSettings}
        password={password}
        passwordConfirm={passwordConfirm}
        passwordError={passwordError}
        passwordMismatch={passwordMismatch}
        setPassword={setPassword}
        setPasswordConfirm={setPasswordConfirm}
        setUsername={setUsername}
        username={username}
      />
    </div>
  );
}
