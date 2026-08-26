import { useState } from "react";
import {
      ChevronDown,
      ChevronRight,
      Copy,
      Pencil,
      Plus,
      Trash2,
} from "lucide-react";

import {
      IconActionButton,
      IconActionGroup,
} from "@/components/IconActionGroup";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TableCell, TableRow } from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { formatNumber } from "@/i18n/format";
import type { Endpoint, EndpointReferenceItem } from "@/lib/types";
import { copyTextToClipboard } from "@/lib/clipboard";
import { cn } from "@/lib/utils";
import type { EndpointTableProps } from "./EndpointTable";
import {
      MobileReferenceDisclosure,
      ReferenceCell,
      ReferenceDisclosureRow,
} from "./EndpointReferenceDisclosure";
import {
      summaryFor,
      type EndpointReferenceDetailState,
      type EndpointReferenceSummaryState,
} from "./useEndpointReferences";

function KeyIdentityCell({
      endpoint,
      formatTime,
}: {
      endpoint: Endpoint;
      formatTime: EndpointTableProps["formatTime"];
}) {
      const { messages } = useLocale();
      const copy = messages.endpoints;
      if (!endpoint.has_api_key) {
            return (
                  <span className="text-xs text-muted-foreground">
                        {copy.noApiKey}
                  </span>
            );
      }
      return (
            <div className="flex min-w-0 flex-col gap-0.5">
                  <span className="font-mono text-xs text-foreground">
                        {endpoint.api_key_fingerprint ?? "—"}
                  </span>
                  <span className="text-[11px] text-muted-foreground">
                        {endpoint.api_key_updated_at
                              ? copy.keyUpdatedAt(
                                      formatTime(endpoint.api_key_updated_at, {
                                            year: "numeric",
                                            month: "short",
                                            day: "numeric",
                                            hour: "numeric",
                                            minute: "2-digit",
                                      }),
                                )
                              : copy.keyUpdatedUnknown}
                  </span>
            </div>
      );
}

function BaseURLCell({ endpoint }: { endpoint: Endpoint }) {
      const { messages } = useLocale();
      const [copied, setCopied] = useState(false);
      return (
            <div className="flex min-w-0 items-center gap-1">
                  <code
                        tabIndex={0}
                        title={endpoint.base_url}
                        className="block min-w-0 flex-1 truncate rounded border border-transparent px-1 py-0.5 font-mono text-xs text-foreground/90 focus-visible:outline-2 focus-visible:outline-ring"
                        aria-label={`${messages.endpoints.baseUrl}: ${endpoint.base_url}`}
                  >
                        {endpoint.base_url}
                  </code>
                  <IconActionButton
                        type="button"
                        size="icon"
                        className="size-6"
                        aria-label={`${messages.endpoints.baseUrl}: ${endpoint.base_url} — 复制`}
                        title="复制"
                        onClick={() => {
                              void copyTextToClipboard(endpoint.base_url);
                              setCopied(true);
                              window.setTimeout(() => setCopied(false), 1200);
                        }}
                  >
                        {copied ? (
                              <Badge
                                    variant="outline"
                                    className="h-4 px-1 text-[10px]"
                              >
                                    ✓
                              </Badge>
                        ) : (
                              <Copy />
                        )}
                  </IconActionButton>
            </div>
      );
}

function EndpointRowGroup({
      endpoint,
      detailState,
      expanded,
      formatTime,
      onAttach,
      onDelete,
      onDuplicate,
      onEdit,
      onLoadMore,
      onOpenReferences,
      onOrphanCleanup,
      onRetryDetail,
      onRetryRow,
      summaryState,
}: {
      endpoint: Endpoint;
      detailState: EndpointReferenceDetailState | undefined;
      expanded: boolean;
      formatTime: EndpointTableProps["formatTime"];
      onAttach: (endpoint: Endpoint) => void;
      onDelete: (endpoint: Endpoint) => void;
      onDuplicate: (endpoint: Endpoint) => void;
      onEdit: (endpoint: Endpoint) => void;
      onLoadMore: (endpointId: number) => void;
      onOpenReferences: () => void;
      onOrphanCleanup: (
            endpoint: Endpoint,
            item: EndpointReferenceItem,
      ) => void;
      /** Re-read this endpoint's reference detail after a failed read. */
      onRetryDetail: (endpointId: number) => void;
      onRetryRow: () => void;
      summaryState: EndpointReferenceSummaryState | undefined;
}) {
      const { messages } = useLocale();
      const copy = messages.endpointsUi;
      const pageCopy = messages.endpointsPage;
      return (
            <>
                  <TableRow
                        data-testid={`endpoint-row-${endpoint.id}`}
                        data-expanded={expanded ? "true" : undefined}
                        className={cn(expanded && "bg-inset/40")}
                  >
                        <TableCell>
                              <div className="flex min-w-0 flex-col gap-0.5">
                                    <span
                                          className="truncate text-sm font-medium text-foreground"
                                          title={endpoint.name}
                                    >
                                          {endpoint.name}
                                    </span>
                                    <span
                                          className="truncate font-mono text-[11px] text-muted-foreground lg:hidden"
                                          title={endpoint.base_url}
                                    >
                                          {endpoint.base_url}
                                    </span>
                                    <span className="text-[11px] text-muted-foreground sm:hidden">
                                          {copy.created(
                                                formatTime(
                                                      endpoint.created_at,
                                                      {
                                                            year: "numeric",
                                                            month: "short",
                                                            day: "numeric",
                                                      },
                                                ),
                                          )}
                                    </span>
                              </div>
                        </TableCell>
                        <TableCell className="hidden lg:table-cell">
                              <BaseURLCell endpoint={endpoint} />
                        </TableCell>
                        <TableCell className="hidden sm:table-cell">
                              <KeyIdentityCell
                                    endpoint={endpoint}
                                    formatTime={formatTime}
                              />
                        </TableCell>
                        <ReferenceCell
                              endpoint={endpoint}
                              summaryState={summaryState}
                              detailState={detailState}
                              onOpen={onOpenReferences}
                              onRetryRow={onRetryRow}
                        />
                        <TableCell>
                              <time
                                    dateTime={endpoint.updated_at}
                                    className="text-xs text-muted-foreground"
                              >
                                    {formatTime(endpoint.updated_at, {
                                          year: "numeric",
                                          month: "short",
                                          day: "numeric",
                                          hour: "numeric",
                                          minute: "2-digit",
                                    })}
                              </time>
                        </TableCell>
                        <TableCell className="text-right">
                              <IconActionGroup className="justify-end">
                                    <IconActionButton
                                          type="button"
                                          size="icon"
                                          aria-label={`${pageCopy.attachToModel}: ${endpoint.name}`}
                                          title={pageCopy.attachToModel}
                                          onClick={() => onAttach(endpoint)}
                                    >
                                          <Plus />
                                    </IconActionButton>
                                    <IconActionButton
                                          type="button"
                                          size="icon"
                                          aria-label={copy.duplicateEndpoint(
                                                endpoint.name,
                                          )}
                                          onClick={() => onDuplicate(endpoint)}
                                    >
                                          <Copy />
                                    </IconActionButton>
                                    <IconActionButton
                                          type="button"
                                          size="icon"
                                          aria-label={copy.editEndpoint(
                                                endpoint.name,
                                          )}
                                          onClick={() => onEdit(endpoint)}
                                    >
                                          <Pencil />
                                    </IconActionButton>
                                    <IconActionButton
                                          type="button"
                                          size="icon"
                                          aria-label={copy.deleteEndpointDescription(
                                                endpoint.name,
                                          )}
                                          destructive
                                          onClick={() => onDelete(endpoint)}
                                    >
                                          <Trash2 />
                                    </IconActionButton>
                              </IconActionGroup>
                        </TableCell>
                  </TableRow>
                  {expanded ? (
                        <ReferenceDisclosureRow
                              endpoint={endpoint}
                              detailState={detailState}
                              onLoadMore={onLoadMore}
                              onOrphanCleanup={onOrphanCleanup}
                              onRetryDetail={onRetryDetail}
                        />
                  ) : null}
            </>
      );
}

function MobileEndpointCard({
      endpoint,
      detailState,
      expanded,
      formatTime,
      onAttach,
      onDelete,
      onDuplicate,
      onEdit,
      onLoadMore,
      onOpenReferences,
      onOrphanCleanup,
      onRetryDetail,
      onRetryRow,
      summaryState,
}: {
      endpoint: Endpoint;
      detailState: EndpointReferenceDetailState | undefined;
      expanded: boolean;
      formatTime: EndpointTableProps["formatTime"];
      onAttach: (endpoint: Endpoint) => void;
      onDelete: (endpoint: Endpoint) => void;
      onDuplicate: (endpoint: Endpoint) => void;
      onEdit: (endpoint: Endpoint) => void;
      onLoadMore: (endpointId: number) => void;
      onOpenReferences: () => void;
      onOrphanCleanup: (
            endpoint: Endpoint,
            item: EndpointReferenceItem,
      ) => void;
      onRetryDetail: (endpointId: number) => void;
      onRetryRow: () => void;
      summaryState: EndpointReferenceSummaryState | undefined;
}) {
      const { messages } = useLocale();
      const copy = messages.endpoints;
      const uiCopy = messages.endpointsUi;
      const pageCopy = messages.endpointsPage;
      const summary = summaryFor(summaryState);

      return (
            <article
                  className="flex flex-col gap-3 px-4 py-3"
                  data-testid={`endpoint-mobile-card-${endpoint.id}`}
            >
                  <dl className="flex flex-col gap-2">
                        <div className="flex items-start justify-between gap-2">
                              <dt className="sr-only">{copy.name}</dt>
                              <dd className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">
                                    {endpoint.name}
                              </dd>
                              <IconActionGroup className="shrink-0">
                                    <IconActionButton
                                          type="button"
                                          size="icon"
                                          aria-label={`${pageCopy.attachToModel}: ${endpoint.name}`}
                                          title={pageCopy.attachToModel}
                                          onClick={() => onAttach(endpoint)}
                                    >
                                          <Plus />
                                    </IconActionButton>
                                    <IconActionButton
                                          type="button"
                                          size="icon"
                                          aria-label={uiCopy.duplicateEndpoint(
                                                endpoint.name,
                                          )}
                                          onClick={() => onDuplicate(endpoint)}
                                    >
                                          <Copy />
                                    </IconActionButton>
                                    <IconActionButton
                                          type="button"
                                          size="icon"
                                          aria-label={uiCopy.editEndpoint(
                                                endpoint.name,
                                          )}
                                          onClick={() => onEdit(endpoint)}
                                    >
                                          <Pencil />
                                    </IconActionButton>
                                    <IconActionButton
                                          type="button"
                                          size="icon"
                                          aria-label={uiCopy.deleteEndpointDescription(
                                                endpoint.name,
                                          )}
                                          destructive
                                          onClick={() => onDelete(endpoint)}
                                    >
                                          <Trash2 />
                                    </IconActionButton>
                              </IconActionGroup>
                        </div>
                        <div className="flex flex-col gap-1">
                              <dt className="sr-only">{copy.baseUrl}</dt>
                              <dd
                                    className="truncate font-mono text-xs text-muted-foreground"
                                    title={endpoint.base_url}
                              >
                                    {endpoint.base_url}
                              </dd>
                        </div>
                        <div className="flex flex-col gap-1">
                              <dt className="sr-only">{copy.apiKey}</dt>
                              <dd className="font-mono text-xs text-foreground">
                                    {endpoint.has_api_key
                                          ? (endpoint.api_key_fingerprint ??
                                            "—")
                                          : copy.noApiKey}
                              </dd>
                        </div>
                        <div className="flex flex-col gap-1">
                              <dt className="sr-only">
                                    {copy.directReferences}
                              </dt>
                              <dd>
                                    <button
                                          type="button"
                                          disabled={!summary}
                                          aria-expanded={expanded}
                                          aria-controls={`endpoint-references-${endpoint.id}`}
                                          aria-label={uiCopy.openReferences(
                                                endpoint.name,
                                                summary
                                                      ? String(
                                                              summary.direct_reference_count,
                                                        )
                                                      : summaryState?.status ===
                                                          "error"
                                                        ? copy.referencesLoadFailed
                                                        : copy.referencesLoading,
                                          )}
                                          className="flex min-w-0 items-center gap-1 text-left disabled:cursor-default"
                                          onClick={
                                                summary
                                                      ? onOpenReferences
                                                      : undefined
                                          }
                                    >
                                          {summary ? (
                                                expanded ? (
                                                      <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
                                                ) : (
                                                      <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
                                                )
                                          ) : null}
                                          {summary?.direct_reference_count ===
                                          0 ? (
                                                <span className="text-xs text-muted-foreground">
                                                      {copy.refsZero}
                                                </span>
                                          ) : summary ? (
                                                <span className="flex flex-col gap-0.5">
                                                      <span className="text-xs font-medium text-foreground">
                                                            {copy.refsSummary(
                                                                  formatNumber(
                                                                        summary.direct_reference_count,
                                                                  ),
                                                                  formatNumber(
                                                                        summary.referencing_model_count,
                                                                  ),
                                                            )}
                                                      </span>
                                                      <span className="text-[11px] text-muted-foreground">
                                                            {copy.refsEnabled(
                                                                  formatNumber(
                                                                        summary.enabled_reference_count,
                                                                  ),
                                                            )}
                                                      </span>
                                                      {summaryState?.status ===
                                                      "stale" ? (
                                                            <span className="text-[11px] text-degraded">
                                                                  {
                                                                        copy.referencesMayBeStale
                                                                  }
                                                            </span>
                                                      ) : null}
                                                </span>
                                          ) : summaryState?.status ===
                                            "error" ? (
                                                <span
                                                      className="text-xs text-failing"
                                                      title={
                                                            pageCopy.referenceUnknownRowReason
                                                      }
                                                >
                                                      {
                                                            pageCopy.referenceUnknownRow
                                                      }
                                                </span>
                                          ) : (
                                                <span className="text-xs text-muted-foreground">
                                                      {copy.referencesLoading}
                                                </span>
                                          )}
                                    </button>
                                    {summaryState?.status === "error" ? (
                                          <Button
                                                type="button"
                                                variant="outline"
                                                size="xs"
                                                className="mt-1"
                                                onClick={onRetryRow}
                                          >
                                                {pageCopy.referenceRetryRow}
                                          </Button>
                                    ) : null}
                              </dd>
                        </div>
                        <div className="flex flex-col gap-1">
                              <dt className="sr-only">{copy.lastModified}</dt>
                              <dd>
                                    <time
                                          dateTime={endpoint.updated_at}
                                          className="text-xs text-muted-foreground"
                                    >
                                          {formatTime(endpoint.updated_at, {
                                                year: "numeric",
                                                month: "short",
                                                day: "numeric",
                                                hour: "numeric",
                                                minute: "2-digit",
                                          })}
                                    </time>
                              </dd>
                        </div>
                  </dl>
                  {expanded ? (
                        // 展开区是行的从属内容，用 inset 底色把它与行卡本身区分开，
                        // 而不是只画一圈与外层同色的边框。
                        <div className="overflow-hidden rounded-lg border border-border bg-inset">
                              <MobileReferenceDisclosure
                                    endpoint={endpoint}
                                    detailState={detailState}
                                    onLoadMore={onLoadMore}
                                    onOrphanCleanup={onOrphanCleanup}
                                    onRetryDetail={onRetryDetail}
                              />
                        </div>
                  ) : null}
            </article>
      );
}

export { BaseURLCell, EndpointRowGroup, KeyIdentityCell, MobileEndpointCard };
