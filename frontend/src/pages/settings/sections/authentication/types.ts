import type { AuthSettings } from "@/lib/types";

export interface AuthenticationSectionProps {
  authSettings: AuthSettings | null;
  authEnabled: boolean;
  username: string;
  setUsername: (value: string) => void;
  email: string;
  setEmail: (value: string) => void;
  password: string;
  passwordError: string | null;
  setPassword: (value: string) => void;
  passwordConfirm: string;
  passwordMismatch: boolean;
  setPasswordConfirm: (value: string) => void;
  emailVerificationOtp: string;
  setEmailVerificationOtp: (value: string) => void;
  sendingEmailVerification: boolean;
  confirmingEmailVerification: boolean;
  authSaving: boolean;
  onRequestEmailVerification: () => Promise<void>;
  onConfirmEmailVerification: () => Promise<void>;
  onSaveAuthSettings: (nextEnabled?: boolean) => Promise<void>;
}

export interface PasskeyCredential {
  id: number;
  device_name: string | null;
  backup_eligible: boolean | null;
  backup_state: boolean | null;
  created_at: string;
  last_used_at: string | null;
}
