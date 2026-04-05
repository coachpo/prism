import { useEffect, useState, type ReactNode } from "react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { api } from "@/lib/api";
import type { LoadbalanceEventDetail } from "@/lib/types";
import { EventTypeBadge, FailureKindBadge } from "./LoadbalanceBadges";

interface LoadbalanceEventDetailSheetProps {
  eventId: number | null;
  onClose: () => void;
}

function DetailRow({
  fallback,
  label,
  value,
}: {
  fallback: string;
  label: string;
  value: ReactNode;
}) {
  return (
    <div className="grid gap-1 border-b py-3 last:border-b-0 sm:grid-cols-[128px,1fr] sm:gap-4">
      <span className="font-medium text-muted-foreground">{label}</span>
      <div className="text-sm text-foreground [overflow-wrap:anywhere] sm:text-right">
        {value ?? fallback}
      </div>
    </div>
  );
}

export function LoadbalanceEventDetailSheet({
  eventId,
  onClose,
}: LoadbalanceEventDetailSheetProps) {
  const { formatNumber, messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.loadbalanceEvents;
  const [eventCache, setEventCache] = useState<Record<number, LoadbalanceEventDetail>>({});
  const [errorCache, setErrorCache] = useState<Record<number, string>>({});

  const event = eventId === null ? null : eventCache[eventId] ?? null;
  const error = eventId === null ? null : errorCache[eventId] ?? null;
  const loading = eventId !== null && event === null && error === null;

  useEffect(() => {
    if (eventId === null || event !== null || error !== null) {
      return;
    }

    let active = true;

    api.loadbalance
      .getEvent(eventId)
      .then((data) => {
        if (!active) return;
        setEventCache((previous) => ({ ...previous, [eventId]: data }));
      })
      .catch((err) => {
        if (!active) return;
        setErrorCache((previous) => ({
          ...previous,
          [eventId]: err.message || copy.failedToLoadEventDetails,
        }));
      });
    return () => {
      active = false;
    };
  }, [copy.failedToLoadEventDetails, error, event, eventId]);

  const open = eventId !== null;
  const banModeLabel =
    event?.ban_mode === "temporary"
      ? copy.banModeTemporary
      : event?.ban_mode === "manual"
        ? copy.banModeManual
        : event?.ban_mode === "off"
          ? copy.banModeOff
          : null;

  return (
    <Sheet open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <SheetContent className="w-full overflow-y-auto p-0 sm:max-w-xl">
        <SheetHeader className="space-y-1 border-b bg-background/95 px-6 py-5 pr-14 backdrop-blur">
          <SheetTitle>{copy.detailsTitle}</SheetTitle>
          <SheetDescription>{copy.eventId(eventId)}</SheetDescription>
        </SheetHeader>

        <div className="space-y-6 p-6">
          {loading && (
            <div className="space-y-3">
              <p className="sr-only">{copy.loadingEvents}</p>
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          )}

          {error && (
            <div className="rounded-lg border border-red-500/25 bg-red-500/10 p-4 text-sm text-red-700 dark:text-red-400">
              {error}
            </div>
          )}

          {event && !loading && (
            <>
              <section className="space-y-3 rounded-2xl border bg-card p-4">
                <h3 className="text-sm font-semibold">{copy.summary}</h3>
                <div className="space-y-1">
                  <DetailRow
                    fallback={messages.common.notApplicable}
                    label={copy.event}
                    value={
                      <div className="flex flex-col items-start gap-2 sm:items-end">
                        <EventTypeBadge eventType={event.event_type} />
                        <span>{event.summary.event}</span>
                      </div>
                    }
                  />
                  <DetailRow
                    fallback={messages.common.notApplicable}
                    label={copy.reason}
                    value={
                      <div className="flex flex-col items-start gap-2 sm:items-end">
                        <FailureKindBadge failureKind={event.failure_kind} />
                        <span>{event.summary.reason}</span>
                      </div>
                    }
                  />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.operation} value={event.summary.operation} />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.cooldown} value={event.summary.cooldown} />
                  <DetailRow
                    fallback={messages.common.notApplicable}
                    label={copy.created}
                    value={formatTime(event.created_at, {
                      year: "numeric",
                      month: "short",
                      day: "numeric",
                      hour: "numeric",
                      minute: "2-digit",
                      second: "2-digit",
                    })}
                  />
                </div>
              </section>

              <section className="space-y-3 rounded-2xl border bg-card p-4">
                <h3 className="text-sm font-semibold">{copy.context}</h3>
                <div className="space-y-1">
                  <DetailRow fallback={messages.common.notApplicable} label={copy.eventType} value={<EventTypeBadge eventType={event.event_type} />} />
                  <DetailRow
                    fallback={messages.common.notApplicable}
                    label={copy.failureKind}
                    value={<FailureKindBadge failureKind={event.failure_kind} />}
                  />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.modelId} value={event.model_id} />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.connectionId} value={event.connection_id} />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.endpointId} value={event.endpoint_id} />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.vendorId} value={event.vendor_id} />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.profileId} value={event.profile_id} />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.consecutiveFailures} value={event.consecutive_failures} />
                </div>
              </section>

              <section className="space-y-3 rounded-2xl border bg-card p-4">
                <h3 className="text-sm font-semibold">{copy.failoverConfiguration}</h3>
                <div className="space-y-1">
                  <DetailRow fallback={messages.common.notApplicable} label={copy.failureThreshold} value={event.failure_threshold} />
                  <DetailRow
                    fallback={messages.common.notApplicable}
                    label={copy.backoffMultiplier}
                    value={
                      event.backoff_multiplier !== null
                        ? formatNumber(event.backoff_multiplier, {
                            minimumFractionDigits: 2,
                            maximumFractionDigits: 2,
                          })
                        : null
                    }
                  />
                  <DetailRow fallback={messages.common.notApplicable} label={copy.maxCooldownSeconds} value={event.max_cooldown_seconds} />
                  {event.max_cooldown_strikes !== null ? (
                    <DetailRow fallback={messages.common.notApplicable} label={copy.maxCooldownStrikes} value={event.max_cooldown_strikes} />
                  ) : null}
                  {event.ban_mode !== null ? (
                    <DetailRow fallback={messages.common.notApplicable} label={copy.banMode} value={banModeLabel} />
                  ) : null}
                  {event.banned_until_at ? (
                    <DetailRow
                      fallback={messages.common.notApplicable}
                      label={copy.bannedUntil}
                      value={formatTime(event.banned_until_at, {
                        year: "numeric",
                        month: "short",
                        day: "numeric",
                        hour: "numeric",
                        minute: "2-digit",
                        second: "2-digit",
                      })}
                    />
                  ) : null}
                </div>
              </section>
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}
