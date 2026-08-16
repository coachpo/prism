import { createContext } from "react";

import type { LoginSessionDuration } from "@/lib/types";
import type { AuthPhase } from "@/context/auth/sessionCoordinator";

export type AuthContextValue = {
  authEnabled: boolean;
  authenticated: boolean;
  loading: boolean;
  username: string | null;
  phase: AuthPhase;
  refreshAuth: () => Promise<void>;
  login: (username: string, password: string, sessionDuration: LoginSessionDuration) => Promise<void>;
  logout: () => Promise<void>;
  retryLogout: () => Promise<void>;
  retryRecovery: () => Promise<void>;
};

export const AuthContext = createContext<AuthContextValue | undefined>(undefined);
