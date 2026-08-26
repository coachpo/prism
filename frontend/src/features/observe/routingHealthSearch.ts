export type RoutingHealthSearch = Record<string, unknown>;

export type RoutingHealthSearchUpdater = (
  patch: RoutingHealthSearch,
  replace?: boolean,
) => void;
