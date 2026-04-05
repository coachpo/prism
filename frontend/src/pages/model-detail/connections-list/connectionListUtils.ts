import type { Connection } from "@/lib/types";
import { getConnectionName } from "../modelDetailMetricsAndPaths";

export function normalizeConnectionSearch(search: string): string {
  return search.trim().toLowerCase();
}

export function filterAndSortConnections(
  connections: Connection[],
  normalizedSearch: string,
): Connection[] {
  const visibleConnections = normalizedSearch
    ? connections.filter((connection) => matchesConnectionSearch(connection, normalizedSearch))
    : connections;

  return [...visibleConnections].sort(
    (left, right) => left.priority - right.priority || left.id - right.id,
  );
}

function matchesConnectionSearch(connection: Connection, normalizedSearch: string): boolean {
  return (
    getConnectionName(connection).toLowerCase().includes(normalizedSearch) ||
    (connection.endpoint?.name || "").toLowerCase().includes(normalizedSearch) ||
    (connection.endpoint?.base_url || "").toLowerCase().includes(normalizedSearch)
  );
}
