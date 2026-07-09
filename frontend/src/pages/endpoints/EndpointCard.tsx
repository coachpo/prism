import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import type { Endpoint, ModelConfigListItem } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  ArrowDown,
  ArrowUp,
  Copy,
  Globe2,
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
  canMoveDown?: boolean;
  canMoveUp?: boolean;
  isDuplicating?: boolean;
  onDelete?: (endpoint: Endpoint) => void | Promise<void>;
  onDuplicate?: (endpoint: Endpoint) => void | Promise<void>;
  onEdit?: (endpoint: Endpoint) => void | Promise<void>;
  onMoveDown?: (endpoint: Endpoint) => void | Promise<void>;
  onMoveUp?: (endpoint: Endpoint) => void | Promise<void>;
}

export function EndpointCardView({
  endpoint,
  formatTime,
  models,
  canMoveDown = false,
  canMoveUp = false,
  isDuplicating = false,
  onDelete,
  onDuplicate,
  onEdit,
  onMoveDown,
  onMoveUp,
}: EndpointCardViewProps) {
  const { messages } = useLocale();
  const copy = messages.endpointsUi;
  const reorderCopy = messages.endpoints;
  const moveUpLabel = reorderCopy.moveUp(endpoint.name);
  const moveDownLabel = reorderCopy.moveDown(endpoint.name);
  const maskedKey = getMaskedApiKey(endpoint);
  const createdAt = formatTime(endpoint.created_at, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });

  return (
    <Card
      className="operator-section-surface relative overflow-hidden transition-[border-color,background-color] hover:border-primary/30 hover:bg-surface-container-low"
    >
      <div className="relative flex flex-col gap-3 p-3 sm:flex-row sm:items-center sm:gap-4">
        <div className="flex min-w-0 items-center gap-3 sm:w-[200px] lg:w-[240px] shrink-0">
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

        <div className="flex shrink-0 items-center justify-end sm:ml-2">
          <IconActionGroup>
            <IconActionButton
              type="button"
              size="icon"
              aria-label={moveUpLabel}
              title={moveUpLabel}
              disabled={!canMoveUp}
              onClick={() => {
                void onMoveUp?.(endpoint);
              }}
            >
              <ArrowUp />
            </IconActionButton>
            <IconActionButton
              type="button"
              size="icon"
              aria-label={moveDownLabel}
              title={moveDownLabel}
              disabled={!canMoveDown}
              onClick={() => {
                void onMoveDown?.(endpoint);
              }}
            >
              <ArrowDown />
            </IconActionButton>
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
      </div>
    </Card>
  );
}
