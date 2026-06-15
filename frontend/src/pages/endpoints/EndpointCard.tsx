import type { ButtonHTMLAttributes } from "react";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import type { Endpoint, ModelConfigListItem } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  Copy,
  Globe2,
  GripVertical,
  KeyRound,
  Loader2,
  Pencil,
  Trash2,
} from "lucide-react";
import {
  getMaskedApiKey,
  getModelBadgeClass,
} from "./endpointCardHelpers";

export interface EndpointCardViewProps {
  endpoint: Endpoint;
  formatTime: (isoString: string, options?: Intl.DateTimeFormatOptions) => string;
  models: ModelConfigListItem[];
  isDragging?: boolean;
  isOverlay?: boolean;
  reorderDisabled?: boolean;
  isDuplicating?: boolean;
  dragHandleAttributes?: ButtonHTMLAttributes<HTMLButtonElement>;
  dragHandleListeners?: ButtonHTMLAttributes<HTMLButtonElement>;
  dragHandleRef?: ((node: HTMLButtonElement | null) => void) | null;
  onDelete?: (endpoint: Endpoint) => void | Promise<void>;
  onDuplicate?: (endpoint: Endpoint) => void | Promise<void>;
  onEdit?: (endpoint: Endpoint) => void | Promise<void>;
}

type SortableEndpointCardProps = EndpointCardViewProps;

export function EndpointCardView({
  endpoint,
  formatTime,
  models,
  isDragging = false,
  isOverlay = false,
  reorderDisabled = false,
  isDuplicating = false,
  dragHandleAttributes,
  dragHandleListeners,
  dragHandleRef,
  onDelete,
  onDuplicate,
  onEdit,
}: EndpointCardViewProps) {
  const { messages } = useLocale();
  const copy = messages.endpointsUi;
  const maskedKey = getMaskedApiKey(endpoint);
  const createdAt = formatTime(endpoint.created_at, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });

  return (
    <Card
      className={cn(
        "operator-section-surface relative overflow-hidden transition-[border-color,background-color,opacity,transform] hover:border-primary/30 hover:bg-surface-container-low",
        isDragging && "border-dashed border-primary/40 bg-surface-container opacity-30",
        isOverlay && "scale-[1.01] cursor-grabbing border-primary/50 shadow-operator-panel ring-2 ring-primary/30"
      )}
    >
      <div className="relative flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:gap-4">
        <div className="flex min-w-0 items-center gap-3 sm:w-[200px] lg:w-[240px] shrink-0">
          <button
            type="button"
            ref={dragHandleRef ?? undefined}
            disabled={reorderDisabled || isOverlay}
            className={cn(
              "flex size-[var(--density-control-h-sm)] shrink-0 touch-none items-center justify-center rounded-md border border-transparent bg-surface-container-low text-muted-foreground transition-colors",
              !reorderDisabled && !isOverlay && "cursor-grab hover:text-foreground active:cursor-grabbing",
              (reorderDisabled || isOverlay) && "cursor-default opacity-60",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            )}
            aria-label={copy.dragToReorder(endpoint.name)}
            {...(dragHandleAttributes ?? {})}
            {...(dragHandleListeners ?? {})}
          >
            <GripVertical />
          </button>

          <div className="min-w-0 flex-1">
            <h3 className="truncate text-sm font-semibold text-foreground" title={endpoint.name}>
              {endpoint.name}
            </h3>
            <p className="mt-0.5 text-[11px] text-muted-foreground">
              {copy.created(createdAt)}
            </p>
          </div>
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-start sm:gap-4 lg:gap-6">
          <div className="flex min-w-0 flex-1 flex-col gap-1.5 sm:min-w-64">
            <div className="flex items-center gap-2 text-xs">
              <Globe2 className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="block min-w-0 flex-1 truncate font-mono text-foreground/90" title={endpoint.base_url}>
                {endpoint.base_url}
              </span>
            </div>
            <div className="flex items-center gap-2 text-xs">
              <KeyRound className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="block min-w-0 flex-1 truncate font-mono text-foreground/90" title={maskedKey}>
                {maskedKey}
              </span>
            </div>
          </div>

          <div className="flex min-w-0 flex-1 flex-col gap-1.5 sm:min-w-72 sm:flex-[2_1_18rem]">
            <div className="flex items-center gap-1.5">
              <span className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                {copy.models}
              </span>
              <Badge
                variant="outline"
                className="h-4 bg-surface-container px-1.5 text-[10px] text-muted-foreground"
              >
                {models.length}
              </Badge>
            </div>

            <div className="flex min-w-0 flex-wrap gap-1">
              {models.length > 0 ? (
                models.map((model) => (
                  <Badge
                    key={model.id}
                    variant="outline"
                    className={cn(
                      "max-w-full rounded-full border px-1.5 py-0 text-[10px] font-medium",
                      getModelBadgeClass(model)
                    )}
                    title={model.display_name || model.model_id}
                  >
                    <span className="block max-w-full truncate">{model.display_name || model.model_id}</span>
                  </Badge>
                ))
              ) : (
                <span className="text-[10px] italic text-muted-foreground">
                  {copy.none}
                </span>
              )}
            </div>
          </div>
        </div>

        {!isOverlay ? (
          <div className="flex shrink-0 items-center justify-end sm:ml-2">
            <IconActionGroup>
                <IconActionButton
                  type="button"
                  size="icon"
                  aria-label={copy.duplicateEndpoint(endpoint.name)}
                  disabled={isDuplicating}
                onClick={() => {
                  void onDuplicate?.(endpoint);
                }}
              >
                {isDuplicating ? (
                  <Loader2 className="animate-spin" />
                ) : (
                  <Copy />
                )}
              </IconActionButton>
                <IconActionButton
                  type="button"
                  size="icon"
                  aria-label={copy.editEndpoint(endpoint.name)}
                  onClick={() => {
                  void onEdit?.(endpoint);
                }}
              >
                <Pencil />
              </IconActionButton>
                <IconActionButton
                  type="button"
                  size="icon"
                  aria-label={copy.deleteEndpointDescription(endpoint.name)}
                  destructive
                onClick={() => {
                  void onDelete?.(endpoint);
                }}
              >
                <Trash2 />
              </IconActionButton>
            </IconActionGroup>
          </div>
        ) : null}
      </div>
    </Card>
  );
}

export function SortableEndpointCard({ endpoint, reorderDisabled = false, ...props }: SortableEndpointCardProps) {
  const {
    attributes,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: endpoint.id,
    disabled: reorderDisabled,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <div ref={setNodeRef} style={style} className="h-full">
      <EndpointCardView
        {...props}
        endpoint={endpoint}
        isDragging={isDragging}
        reorderDisabled={reorderDisabled}
        dragHandleAttributes={attributes as ButtonHTMLAttributes<HTMLButtonElement>}
        dragHandleListeners={listeners as ButtonHTMLAttributes<HTMLButtonElement> | undefined}
        dragHandleRef={setActivatorNodeRef}
      />
    </div>
  );
}
