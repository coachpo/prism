import { useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@/components/ui/select";
import {
  OperatorCallout,
  OperatorLoadingState,
  OperatorMissingValue,
} from "@/shared/design-system";
import { useRequestLogAuditRequest } from "./useRequestLogAuditRequest";
import { useRequestLogAuditList } from "./useRequestLogAuditList";
import { auditScopedDurationMs, auditScopedStatusCode } from "./auditLogView";
import { formatDurationMs } from "./requestLogMetricPresentation";

export function RequestComparisonSide({
  requestId,
  side,
  onLoad,
  loading,
  onCancel,
}: {
  requestId: string;
  side: string;
  onLoad: (id: number) => void;
  loading: boolean;
  onCancel: () => void;
}) {
  const { messages } = useLocale();
  const copy = messages.requestComparison;
  const { format } = useTimezone();
  const [cursor, setCursor] = useState("");
  const [selected, setSelected] = useState("");
  const request = useRequestLogAuditRequest({ requestId });
  const { list, retryList } = useRequestLogAuditList({
    requestId,
    cursor,
    ...request,
  });
  const summary = request.request.request?.summary;
  const error = request.request.error || list.error;
  return (
    <section className="min-w-0 flex flex-col gap-3" aria-label={side}>
      <h3>
        {side} <span className="font-mono">#{requestId}</span>
      </h3>
      {request.request.phase === "loading" ? (
        <OperatorLoadingState title={copy.loading} />
      ) : null}
      {summary ? (
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
          {[
            [copy.model, summary.ingress_model_id],
            [copy.target, summary.attempt_target_model_id],
            [copy.status, auditScopedStatusCode(summary)],
            [copy.duration, formatDurationMs(auditScopedDurationMs(summary))],
            [copy.time, format(summary.created_at)],
          ].map(([label, value]) => (
            <div key={String(label)} className="contents">
              <dt>{label}</dt>
              <dd className="font-mono break-all">
                {value ?? (
                  <OperatorMissingValue reason={messages.honesty.noValue} />
                )}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
      {request.request.fetchedAt ? <p className="text-xs text-muted-foreground">{messages.freshness.updatedAt(format(request.request.fetchedAt))}</p> : null}
      {error ? (
        <OperatorCallout
          intent="danger"
          description={error}
          action={
            <Button
              variant="outline"
              onClick={() => {
                request.retryRequest();
                retryList();
              }}
            >
              {messages.common.retry}
            </Button>
          }
        />
      ) : null}
      {!error &&
      request.request.phase !== "loading" &&
      (request.request.phase !== "ready" || list.phase === "empty") ? (
        <OperatorCallout
          description={
            request.request.captureMode === "disabled"
              ? messages.requestLogs.auditDisabledAtRequest
              : copy.unavailable
          }
        />
      ) : null}
      {list.coverage?.complete === false ? (
        <OperatorCallout intent="warning" description={copy.coverage} />
      ) : null}
      {list.phase === "loading" ? (
        <OperatorLoadingState title={copy.loading} />
      ) : null}
      {list.phase === "ready" ? (
        <>
          <Field>
            <FieldLabel>{copy.capture}</FieldLabel>
            <Select
              value={selected}
              onValueChange={(value) => {
                setSelected(value);
                onCancel();
              }}
            >
              <SelectTrigger aria-label={`${side} ${copy.capture}`}>
                <SelectValue placeholder={copy.capture} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {list.items.map((item) => (
                    <SelectItem key={item.id} value={String(item.id)}>
                      {copy.record(item.id)} ·{" "}
                      {item.row_kind === "upstream"
                        ? copy.upstream
                        : item.row_kind === "planning" ||
                            item.row_kind === "admission"
                          ? copy.gateway
                          : copy.legacy}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Button
            variant="outline"
            disabled={
              loading ||
              !list.items.some((item) => String(item.id) === selected)
            }
            onClick={() => onLoad(Number(selected))}
          >
            {copy.load}
          </Button>
        </>
      ) : null}
      {loading ? (
        <Button variant="outline" onClick={onCancel}>
          {copy.cancel}
        </Button>
      ) : null}
      <div className="flex flex-wrap gap-2">
        {cursor ? (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              setSelected("");
              setCursor("");
              onCancel();
            }}
          >
            {copy.first}
          </Button>
        ) : null}
        {list.hasMore && list.nextCursor ? (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              setSelected("");
              setCursor(list.nextCursor!);
              onCancel();
            }}
          >
            {copy.more}
          </Button>
        ) : null}
      </div>
    </section>
  );
}
