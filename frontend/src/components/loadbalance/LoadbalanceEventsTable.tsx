import { useTimezone } from "@/hooks/useTimezone";
import type { LoadbalanceEvent } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { OperatorEmptyState, OperatorLoadingState } from "@/shared/design-system";
import { EventTypeBadge, FailureKindBadge } from "./LoadbalanceBadges";

interface LoadbalanceEventsTableProps {
  events: LoadbalanceEvent[];
  loading: boolean;
  total: number;
  offset: number;
  limit: number;
  onSelectEvent: (eventId: number) => void;
  onPreviousPage: () => void;
  onNextPage: () => void;
}

export function LoadbalanceEventsTable({
  events,
  loading,
  total,
  offset,
  limit,
  onSelectEvent,
  onPreviousPage,
  onNextPage,
}: LoadbalanceEventsTableProps) {
  const { formatNumber, messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.loadbalanceEvents;
  const rangeLabel =
    total === 0
      ? copy.showingEvents("0", "0", "0")
      : copy.showingEvents(
          formatNumber(offset + 1),
          formatNumber(Math.min(offset + limit, total)),
          formatNumber(total),
        );

  return (
    <div className="overflow-hidden rounded-xl border border-outline-variant bg-surface">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{copy.tableId}</TableHead>
            <TableHead>{copy.tableEvent}</TableHead>
            <TableHead>{copy.tableFailure}</TableHead>
            <TableHead>{copy.tableConnection}</TableHead>
            <TableHead>{copy.tableFailures}</TableHead>
            <TableHead>{copy.tableCooldown}</TableHead>
            <TableHead>{copy.tableCreated}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableRow>
              <TableCell colSpan={7} className="p-4">
                <OperatorLoadingState
                  title={copy.loadingEvents}
                  description="Fetching the latest retry, ban, and recovery events."
                />
              </TableCell>
            </TableRow>
          ) : null}

          {!loading && events.length === 0 ? (
            <TableRow>
              <TableCell colSpan={7} className="p-4">
                <OperatorEmptyState
                  title={copy.noEventsRecorded}
                  description="Try a different model ID or refresh after new traffic is recorded."
                />
              </TableCell>
            </TableRow>
          ) : null}

          {!loading
            ? events.map((event) => (
                <TableRow
                  key={event.id}
                  className="cursor-pointer hover:bg-surface-container-low"
                  onClick={() => onSelectEvent(event.id)}
                >
                  <TableCell className="font-mono text-xs">{event.id}</TableCell>
                  <TableCell>
                    <EventTypeBadge eventType={event.event_type} />
                  </TableCell>
                  <TableCell>
                    <FailureKindBadge failureKind={event.failure_kind} />
                  </TableCell>
                  <TableCell className="font-mono text-xs">#{event.connection_id}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {formatNumber(event.cumulative_retry_attempts)}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {event.next_retry_at
                      ? formatTime(event.next_retry_at, {
                          month: "short",
                          day: "numeric",
                          hour: "numeric",
                          minute: "2-digit",
                        })
                      : messages.common.notApplicable}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatTime(event.created_at, {
                      month: "short",
                      day: "numeric",
                      hour: "numeric",
                      minute: "2-digit",
                    })}
                  </TableCell>
                </TableRow>
              ))
            : null}
        </TableBody>
      </Table>

      <div className="flex items-center justify-between border-t border-outline-variant bg-surface-container-low px-4 py-3">
        <p className="text-xs text-muted-foreground">
          {rangeLabel}
        </p>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={onPreviousPage} disabled={offset === 0}>
            {copy.previous}
          </Button>
          <Button variant="outline" size="sm" onClick={onNextPage} disabled={offset + limit >= total}>
            {copy.next}
          </Button>
        </div>
      </div>
    </div>
  );
}
