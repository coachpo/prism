import type { AuthenticationSectionProps } from "./types";
import { OperatorEmailCard } from "./OperatorEmailCard";
import { RecoveryEmailCard } from "./RecoveryEmailCard";

export function AuthenticationSetupGrid({
  authEnabled,
  authSaving,
  authSettings,
  username,
  setUsername,
  email,
  setEmail,
  password,
  passwordError,
  setPassword,
  passwordConfirm,
  passwordMismatch,
  setPasswordConfirm,
  emailVerificationOtp,
  setEmailVerificationOtp,
  sendingEmailVerification,
  confirmingEmailVerification,
  onRequestEmailVerification,
  onConfirmEmailVerification,
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

      <RecoveryEmailCard
        authSettings={authSettings}
        confirmingEmailVerification={confirmingEmailVerification}
        email={email}
        emailVerificationOtp={emailVerificationOtp}
        onConfirmEmailVerification={onConfirmEmailVerification}
        onRequestEmailVerification={onRequestEmailVerification}
        sendingEmailVerification={sendingEmailVerification}
        setEmail={setEmail}
        setEmailVerificationOtp={setEmailVerificationOtp}
      />
    </div>
  );
}
