import { Loader2 } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import type { Connection, LoadbalanceCurrentStateItem } from "@/lib/types";
import type { FormatTime } from "./connectionCardTypes";

export function ConnectionCardDetails({
  connection,
  currentState,
  formatTime,
  isChecking,
}: {
  connection: Connection;
  currentState?: LoadbalanceCurrentStateItem;
  formatTime: FormatTime;
  isChecking: boolean;
}) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const endpoint = connection.endpoint;
  const maskedKey = endpoint?.masked_api_key || "......";

  return (
    <>
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span className="truncate font-medium">{endpoint?.name ?? copy.unknownEndpoint}</span>
        <span className="text-muted-foreground/70">.</span>
        <span className="font-mono break-all">{endpoint?.base_url}</span>
      </div>

      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span>{copy.keyLabel}: {maskedKey}</span>
        {isChecking ? (
          <span className="inline-flex items-center gap-1">
            <Loader2 className="h-3 w-3 animate-spin" />
            {copy.checkingNow}
          </span>
        ) : connection.last_health_check ? (
          <span>
            {copy.checkedAt(
              formatTime(connection.last_health_check, {
                hour: "numeric",
                minute: "numeric",
                second: "numeric",
              }),
            )}
          </span>
        ) : (
          <span>{copy.notCheckedYet}</span>
        )}
      </div>

      {currentState ? (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {currentState.live_p95_latency_ms !== null ? (
            <span>{copy.liveP95Latency(`${Math.round(currentState.live_p95_latency_ms)}ms`)}</span>
          ) : null}
          {currentState.last_success_at ? (
            <span>
              {copy.lastLiveSuccess(
                formatTime(currentState.last_success_at, {
                  hour: "numeric",
                  minute: "numeric",
                  second: "numeric",
                }),
              )}
            </span>
          ) : null}
          {currentState.next_retry_at ? (
            <span>
              {copy.lastLiveFailure(
                formatTime(currentState.next_retry_at, {
                  hour: "numeric",
                  minute: "numeric",
                  second: "numeric",
                }),
              )}
            </span>
          ) : null}
        </div>
      ) : null}
    </>
  );
}
