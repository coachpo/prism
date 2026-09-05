import { useEffect, useState } from "react";
import { useRouterState } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout } from "@/shared/design-system";
import { readSearchFallback, retireSearchFallbackUnless } from "./searchFallback";

/**
 * 路由 search schema 用 `.catch()` 把非法参数折回默认值，页面因此不再整块崩掉。
 * 但静默回落只是把「链接坏了」从可见变成不可见：操作者会以为自己看到的就是
 * 链接指向的视图。这里把被忽略的参数原样列出来，回落始终是看得见的。
 */
export function SearchFallbackNotice() {
  const { messages } = useLocale();
  const pathname = useRouterState({
    select: (state) => state.location.pathname,
  });
  const [dismissed, setDismissed] = useState<string | null>(null);
  const rejected = readSearchFallback(pathname);
  const signature = rejected.join("、");

  // 换页即失效：留在同一页时（包括路由把地址栏整理干净的那一次）记录必须还在。
  useEffect(() => retireSearchFallbackUnless(pathname), [pathname]);

  if (rejected.length === 0 || dismissed === signature) {
    return null;
  }

  return (
    <OperatorCallout
      intent="warning"
      data-testid="search-fallback-notice"
      title={messages.common.searchParamsIgnoredTitle}
      description={messages.common.searchParamsIgnoredDescription(signature)}
      action={
        <Button variant="ghost" size="sm" onClick={() => setDismissed(signature)}>
          {messages.common.dismiss}
        </Button>
      }
    />
  );
}
