import type { LoginSessionDuration, SessionResponse } from "@/lib/types";

export interface AuthMutationsOptions {
  loginRequest: (payload: {
    username: string;
    password: string;
    session_duration: LoginSessionDuration;
  }) => Promise<SessionResponse>;
  logoutRequest: () => Promise<SessionResponse>;
  setLoading: (loading: boolean) => void;
  applySessionState: (session: SessionResponse) => void;
  beginMutation: () => void;
  endMutation: () => void;
}

export function createAuthMutations(options: AuthMutationsOptions) {
  const login = async (
    username: string,
    password: string,
    sessionDuration: LoginSessionDuration,
  ) => {
    options.beginMutation();

    try {
      const session = await options.loginRequest({
        username,
        password,
        session_duration: sessionDuration,
      });
      options.setLoading(false);
      options.applySessionState(session);
    } finally {
      options.endMutation();
    }
  };

  const logout = async () => {
    options.beginMutation();

    try {
      const session = await options.logoutRequest();
      options.setLoading(false);
      options.applySessionState(session);
    } finally {
      options.endMutation();
    }
  };

  return {
    login,
    logout,
  };
}
