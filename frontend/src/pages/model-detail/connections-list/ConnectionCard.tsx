import { GripVertical } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import { getConnectionName } from "../modelDetailMetricsAndPaths";
import { ConnectionCardActions } from "./ConnectionCardActions";
import { ConnectionCardCooldownState } from "./ConnectionCardCooldownState";
import { ConnectionCardDetails } from "./ConnectionCardDetails";
import { ConnectionCardHeader } from "./ConnectionCardHeader";
import type { ConnectionCardProps } from "./connectionCardTypes";

export function ConnectionCard({
  connection,
  loadbalanceCurrentState,
  isChecking,
  isResettingCooldown,
  isFocused,
  formatTime,
  reorderDisabled,
  isDragging = false,
  isOverlay = false,
  dragHandleAttributes,
  dragHandleListeners,
  dragHandleRef,
  cardRef,
  onEdit,
  onDelete,
  onHealthCheck,
  onResetCooldown,
  onToggleActive,
}: ConnectionCardProps) {
  const connectionName = getConnectionName(connection);
  const { messages } = useLocale();
  const copy = messages.modelDetail;

  return (
    <div
      ref={cardRef}
      tabIndex={cardRef ? -1 : undefined}
      className={cn(
        "group rounded-xl border bg-card p-4 transition-[border-color,box-shadow,opacity,transform] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
        !connection.is_active && "border-border/60 bg-muted/20",
        isFocused && "border-primary/30 bg-muted/20 ring-2 ring-primary/40",
        !isFocused && !isDragging && "hover:border-border",
        isDragging && "border-dashed border-primary/40 bg-muted/30 opacity-30",
        isOverlay && "scale-[1.02] cursor-grabbing border-primary/50 opacity-95 shadow-2xl ring-2 ring-primary/30",
      )}
    >
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-1 items-start gap-3">
          <button
            type="button"
            ref={dragHandleRef ?? undefined}
            disabled={reorderDisabled || isOverlay}
            className={cn(
              "mt-0.5 flex h-11 w-11 shrink-0 touch-none items-center justify-center rounded-lg border border-transparent text-muted-foreground/60 transition-colors sm:h-9 sm:w-9",
              !reorderDisabled && !isOverlay && "cursor-grab hover:text-foreground active:cursor-grabbing",
              (reorderDisabled || isOverlay) && "cursor-default opacity-30",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
            )}
            aria-label={copy.dragToReorderConnection(connectionName)}
            {...(dragHandleAttributes ?? {})}
            {...(dragHandleListeners ?? {})}
          >
            <GripVertical className="h-5 w-5" />
          </button>

          <div className={cn("min-w-0 flex-1 space-y-1.5", !connection.is_active && "opacity-75")}>
            <ConnectionCardHeader
              connection={connection}
              connectionName={connectionName}
              isChecking={isChecking}
            />

            <ConnectionCardDetails
              connection={connection}
              formatTime={formatTime}
              isChecking={isChecking}
            />

            <ConnectionCardCooldownState
              currentState={loadbalanceCurrentState}
              formatTime={formatTime}
              isResetting={isResettingCooldown}
              onResetCooldown={onResetCooldown}
            />
          </div>
        </div>

        {!isOverlay ? (
          <ConnectionCardActions
            connection={connection}
            isChecking={isChecking}
            onEdit={onEdit}
            onDelete={onDelete}
            onHealthCheck={onHealthCheck}
            onToggleActive={onToggleActive}
          />
        ) : null}
      </div>
    </div>
  );
}
