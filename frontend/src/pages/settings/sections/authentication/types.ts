import type { AuthSettings } from "@/lib/types";

export interface AuthenticationSectionProps {
  authSettings: AuthSettings | null;
  authEnabled: boolean;
  username: string;
  setUsername: (value: string) => void;
  password: string;
  passwordError: string | null;
  setPassword: (value: string) => void;
  passwordConfirm: string;
  passwordMismatch: boolean;
  setPasswordConfirm: (value: string) => void;
  authSaving: boolean;
  onSaveAuthSettings: (nextEnabled?: boolean) => Promise<void>;
}
