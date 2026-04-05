import { createContext } from "react";

import type { LoginSessionDuration } from "@/lib/types";

export type AuthContextValue = {
  authEnabled: boolean;
  authenticated: boolean;
  loading: boolean;
  username: string | null;
  refreshAuth: () => Promise<void>;
  login: (username: string, password: string, sessionDuration: LoginSessionDuration) => Promise<void>;
  logout: () => Promise<void>;
};

export const AuthContext = createContext<AuthContextValue | undefined>(undefined);
