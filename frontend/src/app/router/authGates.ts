export interface RouteLocationState {
  pathname: string
  search: string
  hash: string
}

export interface AuthGateState {
  authEnabled: boolean
  authenticated: boolean
  loading: boolean
}

export function buildAuthReturnState(location: RouteLocationState) {
  return {
    from: {
      pathname: location.pathname,
      search: location.search,
      hash: location.hash,
    },
  }
}

export function resolveProtectedRedirect(state: AuthGateState, location: RouteLocationState) {
  if (state.loading || !state.authEnabled || state.authenticated) {
    return null
  }

  return {
    to: "/auth/login" as const,
    state: buildAuthReturnState(location),
  }
}

export function resolvePublicRedirect(state: AuthGateState) {
  if (state.loading || (state.authEnabled && !state.authenticated)) {
    return null
  }

  return "/observe" as const
}
