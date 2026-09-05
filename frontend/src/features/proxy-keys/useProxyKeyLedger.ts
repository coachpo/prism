import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { rewriteQueryKeys } from "@/shared/api/queryKeys";
import { useProxyKeyUsage } from "./useProxyKeyUsage";

export function useProxyKeyLedger() {
  const [visibleKeyIds, setVisibleKeyIds] = useState<number[]>([]);
  const authSettingsQuery = useQuery({
    queryKey: rewriteQueryKeys.global.settingsAuth(),
    queryFn: api.settings.auth.get,
  });
  const proxyKeysQuery = useQuery({
    queryKey: rewriteQueryKeys.global.proxyApiKeys(),
    queryFn: api.settings.auth.proxyKeys.list,
  });
  const usage = useProxyKeyUsage(visibleKeyIds);
  const messages = getStaticMessages();

  const authSettings = authSettingsQuery.data ?? null;
  const proxyKeyList = proxyKeysQuery.data;
  const proxyKeys = useMemo(
    () => proxyKeyList?.items ?? [],
    [proxyKeyList],
  );
  const displayedProxyKeys = useMemo(
    () => [...proxyKeys].sort((left, right) => right.id - left.id),
    [proxyKeys],
  );
  const capacity = proxyKeyList?.capacity ?? null;
  const proxyKeyLimit =
    capacity?.limit ?? authSettings?.proxy_key_limit ?? 100;
  // Never infer capacity from the currently loaded list: a stale/failed list
  // query is not evidence that slots are available.
  const remainingKeys = capacity?.remaining ?? 0;
  const pageLoading =
    authSettingsQuery.isLoading || proxyKeysQuery.isLoading;
  const pageError = proxyKeysQuery.error
    ? proxyKeysQuery.error instanceof Error
      ? proxyKeysQuery.error.message
      : messages.proxyApiKeysData.loadKeysFailed
    : authSettingsQuery.error
      ? authSettingsQuery.error instanceof Error
        ? authSettingsQuery.error.message
        : messages.proxyApiKeysData.loadAuthStatusFailed
      : null;
  const pageErrorTitle = proxyKeysQuery.error
    ? messages.proxyApiKeysData.loadKeysFailed
    : messages.proxyApiKeysData.loadAuthStatusFailed;
  // 读失败但手里还有上次成功的台账时，保留它并标注陈旧；只有从来没读成功过
  // 才整块换成错误卡。丢掉数据等于把「后端挂了」渲染成「这里没有密钥」。
  const hasLastGoodData = proxyKeyList !== undefined;
  const lastSuccessfulAt =
    proxyKeysQuery.dataUpdatedAt > 0
      ? new Date(proxyKeysQuery.dataUpdatedAt).toISOString()
      : null;
  const refetchAuthSettings = authSettingsQuery.refetch;
  const refetchProxyKeys = proxyKeysQuery.refetch;

  const retryPage = useCallback(() => {
    void Promise.all([
      refetchAuthSettings(),
      refetchProxyKeys(),
    ]);
  }, [refetchAuthSettings, refetchProxyKeys]);

  const handleVisibleKeysChange = useCallback((keyIds: number[]) => {
    setVisibleKeyIds((current) =>
      current.length === keyIds.length &&
      current.every((id, index) => id === keyIds[index])
        ? current
        : keyIds,
    );
  }, []);

  return {
    authSettings,
    capacity,
    displayedProxyKeys,
    handleVisibleKeysChange,
    hasLastGoodData,
    lastSuccessfulAt,
    pageError,
    pageErrorTitle,
    pageLoading,
    proxyKeyLimit,
    proxyKeys,
    remainingKeys,
    retryPage,
    retryUsage: usage.refetch,
    usageEntries: usage.entries,
    usageFailed: usage.hasFailure,
  };
}
