import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";

export function useRequestLogProxyApiKeyOptions(selectedId: string) {
  const [search, setSearch] = useState("");
  const query = useQuery({
    queryKey: ["request-logs", "proxy-api-key-options", search, selectedId],
    queryFn: () =>
      api.stats.proxyApiKeyFilterOptions({
        q: search || undefined,
        selected_id: selectedId ? parseInt(selectedId, 10) : undefined,
      }),
  });
  const options = useMemo(() => {
    const items = query.data?.items ?? [];
    const selected = query.data?.selected ?? null;
    if (
      selected &&
      !items.some(
        (item) => item.proxy_api_key_id === selected.proxy_api_key_id,
      )
    ) {
      return [selected, ...items];
    }
    return items;
  }, [query.data]);

  return {
    options,
    search,
    setSearch,
  };
}
