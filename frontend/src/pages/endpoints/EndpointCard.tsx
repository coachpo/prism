import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import type { Endpoint, ModelConfigListItem } from "@/lib/types";
import { cn } from "@/lib/utils";
import {
  ArrowDown,
  ArrowUp,
  Cable,
  Copy,
  Globe2,
  KeyRound,
  Loader2,
  Link2,
  Pencil,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import {
  getMaskedApiKey,
  getModelBadgeClass,
} from "./endpointCardHelpers";

export interface EndpointReferenceView {
  connection_id: number;
  terminal_target_name: string | null;
  model_config_id: number;
  model_id: string;
  model_display_name: string | null;
  api_family: string;
  is_enabled: boolean;
  is_active: boolean;
  openai_text_capability: string | null;
  pricing_template: { id: number; name: string } | null;
}

export interface EndpointCardViewProps {
  endpoint: Endpoint;
  formatTime: (isoString: string, options?: Intl.DateTimeFormatOptions) => string;
  models: ModelConfigListItem[];
  directReferences?: EndpointReferenceView[];
  canMoveDown?: boolean;
  canMoveUp?: boolean;
  isDuplicating?: boolean;
  onAttach?: (endpoint: Endpoint) => void | Promise<void>;
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
  directReferences = [],
  canMoveDown = false,
  canMoveUp = false,
  isDuplicating = false,
  onAttach,
  onDelete,
  onDuplicate,
  onEdit,
  onMoveDown,
  onMoveUp,
}: EndpointCardViewProps) {
  const { messages } = useLocale();
  const copy = messages.endpointsUi;
  const reorderCopy = messages.endpoints;
  const [referencesOpen, setReferencesOpen] = useState(false);
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
              <button
                type="button"
                className="inline-flex items-center gap-1 text-[10px] text-muted-foreground underline-offset-2 hover:underline"
                aria-expanded={referencesOpen}
                aria-controls={`endpoint-references-${endpoint.id}`}
                onClick={() => setReferencesOpen((open) => !open)}
              >
                <Link2 className="size-3" />
                {copy.directReferences(directReferences.length)}
              </button>
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
            {models.length > 0 ? (
              <span className="text-[10px] italic text-muted-foreground">{copy.reachableModelsLabel}</span>
            ) : null}

            {referencesOpen ? (
              <div
                id={`endpoint-references-${endpoint.id}`}
                className="flex flex-col gap-1 rounded-md border border-outline-variant bg-surface-container-low p-2"
                data-testid={`endpoint-direct-references-${endpoint.id}`}
              >
                <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                  {copy.directReferencesTitle}
                </p>
                {directReferences.length === 0 ? (
                  <p className="text-[10px] italic text-muted-foreground">{copy.noDirectReferences}</p>
                ) : (
                  <ul className="flex flex-col gap-1">
                    {directReferences.map((reference) => (
                      <li key={reference.connection_id} className="flex flex-wrap items-center gap-1.5 text-[10px] text-foreground/90">
                        <Cable className="size-3 shrink-0 text-muted-foreground" />
                        <span className="font-medium">{reference.model_display_name || reference.model_id}</span>
                        <span className="text-muted-foreground">→ {reference.terminal_target_name ?? `终端目标 ${reference.connection_id}`}</span>
                        {reference.openai_text_capability ? (
                          <Badge variant="outline" className="h-4 px-1 text-[9px]">
                            {reference.openai_text_capability}
                          </Badge>
                        ) : null}
                        {reference.pricing_template ? (
                          <span className="text-muted-foreground">· {reference.pricing_template.name}</span>
                        ) : null}
                        <span className={reference.is_enabled && reference.is_active ? "text-success" : "text-warning"}>
                          {reference.is_enabled ? "参与路由" : "不参与路由"}
                          {!reference.is_active ? " · 未激活" : ""}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            ) : null}
          </div>
        </div>

        <div className="flex shrink-0 items-center justify-end sm:ml-2">
          <IconActionGroup>
            {onAttach ? (
              <IconActionButton
                type="button"
                size="icon"
                aria-label={copy.attachEndpoint(endpoint.name)}
                title={copy.attachEndpoint(endpoint.name)}
                onClick={() => {
                  void onAttach(endpoint);
                }}
              >
                <Cable />
              </IconActionButton>
            ) : null}
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
