import { useState } from "react";

import {
    Table,
    TableBody,
    TableHead,
    TableHeader,
    TableRow,
} from "@/components/ui/table";
import { OperatorCallout } from "@/shared/design-system";
import { useLocale } from "@/i18n/useLocale";
import type { Endpoint, EndpointReferenceItem } from "@/lib/types";
import {
    SortableTableHead,
} from "@/shared/table/operationalTable";
import type { OperationalSortState } from "@/shared/table/operationalTableState";
import { EndpointRowGroup, MobileEndpointCard } from "./EndpointRows";
import type { EndpointReferenceDetailState } from "./useEndpointReferenceDetails";
import type { EndpointReferenceSummaryState } from "./useEndpointReferenceSummaries";

export type EndpointSortColumn =
    | "name"
    | "updated_at"
    | "direct_reference_count";

type EndpointTableProps = {
    endpoints: Endpoint[];
    details: Record<number, EndpointReferenceDetailState>;
    formatTime: (
        isoString: string,
        options?: Intl.DateTimeFormatOptions,
    ) => string;
    hasIntegrityError: boolean;
    onAttach: (endpoint: Endpoint) => void;
    onDelete: (endpoint: Endpoint) => void;
    onDuplicate: (endpoint: Endpoint) => void;
    onEdit: (endpoint: Endpoint) => void;
    onLoadMore: (endpointId: number) => void;
    onOpenReferences: (endpointId: number) => void;
    onOrphanCleanup: (endpoint: Endpoint, item: EndpointReferenceItem) => void;
    /** Re-read one endpoint's reference detail after a failed read. */
    onRetryDetail: (endpointId: number) => void;
    sort: OperationalSortState<EndpointSortColumn>;
    summaries: Record<number, EndpointReferenceSummaryState>;
    onSort: (column: EndpointSortColumn) => void;
};

export function EndpointTable({
    endpoints,
    details,
    formatTime,
    hasIntegrityError,
    onAttach,
    onDelete,
    onDuplicate,
    onEdit,
    onLoadMore,
    onOpenReferences,
    onOrphanCleanup,
    onRetryDetail,
    onSort,
    sort,
    summaries,
}: EndpointTableProps) {
    const { messages } = useLocale();
    const copy = messages.endpoints;
    const uiCopy = messages.endpointsUi;
    const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set());

    const toggleExpanded = (endpointId: number) => {
        setExpandedIds((current) => {
            const next = new Set(current);
            if (next.has(endpointId)) {
                next.delete(endpointId);
            } else {
                next.add(endpointId);
                onOpenReferences(endpointId);
            }
            return next;
        });
    };

    return (
        <div
            data-testid="endpoints-table"
            data-table-density="compact"
            className="overflow-hidden border-t border-border"
        >
            {hasIntegrityError ? (
                <div className="border-b border-border px-4 py-3">
                    <OperatorCallout
                        intent="danger"
                        title={uiCopy.deleteIntegrityError}
                    />
                </div>
            ) : null}
            {/* Narrow viewport: semantic description-list row cards (no horizontal
          table scroll). The desktop table is hidden below sm. */}
            <div
                className="divide-y divide-border sm:hidden"
                data-testid="endpoints-mobile-cards"
            >
                {endpoints.map((endpoint) => (
                    <MobileEndpointCard
                        key={endpoint.id}
                        endpoint={endpoint}
                        detailState={details[endpoint.id]}
                        expanded={expandedIds.has(endpoint.id)}
                        formatTime={formatTime}
                        onAttach={onAttach}
                        onDelete={onDelete}
                        onDuplicate={onDuplicate}
                        onEdit={onEdit}
                        onLoadMore={onLoadMore}
                        onOpenReferences={() => toggleExpanded(endpoint.id)}
                        onOrphanCleanup={onOrphanCleanup}
                        onRetryDetail={onRetryDetail}
                        summaryState={summaries[endpoint.id]}
                        onRetryRow={() => onOpenReferences(endpoint.id)}
                    />
                ))}
            </div>
            <div
                className="hidden sm:block"
                data-testid="endpoints-table-desktop"
            >
                <Table>
                    <TableHeader>
                        <TableRow>
                            <SortableTableHead
                                sortKey="name"
                                sort={sort}
                                onSort={onSort}
                            >
                                {copy.name}
                            </SortableTableHead>
                            <TableHead className="hidden lg:table-cell">
                                {copy.baseUrl}
                            </TableHead>
                            <TableHead className="hidden sm:table-cell">
                                {copy.apiKey}
                            </TableHead>
                            <SortableTableHead
                                sortKey="direct_reference_count"
                                sort={sort}
                                onSort={onSort}
                            >
                                {copy.directReferences}
                            </SortableTableHead>
                            <SortableTableHead
                                sortKey="updated_at"
                                sort={sort}
                                onSort={onSort}
                            >
                                {copy.lastModified}
                            </SortableTableHead>
                            <TableHead className="text-right">
                                {messages.endpoints.actions}
                            </TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {endpoints.map((endpoint) => {
                            const expanded = expandedIds.has(endpoint.id);
                            const detailState = details[endpoint.id];
                            return (
                                <EndpointRowGroup
                                    key={endpoint.id}
                                    endpoint={endpoint}
                                    detailState={detailState}
                                    expanded={expanded}
                                    formatTime={formatTime}
                                    onAttach={onAttach}
                                    onDelete={onDelete}
                                    onDuplicate={onDuplicate}
                                    onEdit={onEdit}
                                    onLoadMore={onLoadMore}
                                    onOpenReferences={() =>
                                        toggleExpanded(endpoint.id)
                                    }
                                    onOrphanCleanup={onOrphanCleanup}
                                    onRetryDetail={onRetryDetail}
                                    summaryState={summaries[endpoint.id]}
                                    onRetryRow={() =>
                                        onOpenReferences(endpoint.id)
                                    }
                                />
                            );
                        })}
                    </TableBody>
                </Table>
            </div>
        </div>
    );
}

export type { EndpointTableProps };
