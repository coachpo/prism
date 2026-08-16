import type { AuthSettings } from "@/lib/types";
import type { AuthSettingsConfirmation } from "../../useAuthenticationSettingsData";

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
  pendingAuthConfirmation: AuthSettingsConfirmation | null;
  onConfirmAuthSettings: () => Promise<void>;
  onCancelAuthSettingsConfirmation: () => void;
}
