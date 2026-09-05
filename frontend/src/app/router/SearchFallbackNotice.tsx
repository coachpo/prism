import { useRouterState } from "@tanstack/react-router";

import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout } from "@/shared/design-system";
import { collectRejectedSearchKeys } from "./rewriteRoutes";

/**
 * 路由 search schema 用 `.catch()` 把非法参数折回默认值，页面因此不再整块崩掉。
 * 但静默回落只是把「链接坏了」从可见变成不可见：操作者会以为自己看到的就是
 * 链接指向的视图。这里把被忽略的参数原样列出来，回落始终是看得见的。
 */
export function SearchFallbackNotice({
  keys,
  search,
}: {
  keys: readonly string[];
  search: Record<string, unknown>;
}) {
  const { messages } = useLocale();
  const searchStr = useRouterState({
    select: (state) => state.location.searchStr,
  });
  const rejected = collectRejectedSearchKeys(searchStr, search, keys);

  if (rejected.length === 0) {
    return null;
  }

  return (
    <OperatorCallout
      intent="warning"
      data-testid="search-fallback-notice"
      title={messages.common.searchParamsIgnoredTitle}
      description={messages.common.searchParamsIgnoredDescription(
        rejected.join("、"),
      )}
    />
  );
}
