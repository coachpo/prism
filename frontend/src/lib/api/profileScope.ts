type ScopedApiRouteMatcher = {
  exact?: boolean;
  segments: readonly string[];
};

const PROFILE_SCOPED_API_ROUTE_MATCHERS: readonly ScopedApiRouteMatcher[] = [
  { segments: ["models"] },
  { segments: ["endpoints"] },
  { segments: ["connections"] },
  { segments: ["pricing-templates"] },
  { segments: ["stats"] },
  { segments: ["audit"] },
  { segments: ["loadbalance", "strategies"] },
  { segments: ["loadbalance", "current-state"] },
  { segments: ["loadbalance", "events"] },
  { segments: ["loadbalance", "incidents"] },
  { segments: ["settings", "costing"] },
  { exact: true, segments: ["settings", "audit"] },
  { segments: ["config", "header-blocklist-rules"] },
  { segments: ["config", "user-agent-client-rules"] },
];

function getApiPathSegments(path: string): string[] {
  const separatorIndex = path.search(/[?#]/);
  const pathname = separatorIndex === -1 ? path : path.slice(0, separatorIndex);

  if (!pathname.startsWith("/api/")) {
    return [];
  }

  return pathname
    .split("/")
    .filter((segment) => segment.length > 0)
    .slice(1);
}

function matchesSegments(pathSegments: readonly string[], routeSegments: readonly string[]): boolean {
  if (pathSegments.length < routeSegments.length) {
    return false;
  }

  return routeSegments.every((segment, index) => pathSegments[index] === segment);
}

export function isProfileScopedManagementRoute(path: string): boolean {
  const pathSegments = getApiPathSegments(path);

  if (pathSegments.length === 0) {
    return false;
  }

  return PROFILE_SCOPED_API_ROUTE_MATCHERS.some(({ exact = false, segments }) => {
    if (!matchesSegments(pathSegments, segments)) {
      return false;
    }

    return !exact || pathSegments.length === segments.length;
  });
}
