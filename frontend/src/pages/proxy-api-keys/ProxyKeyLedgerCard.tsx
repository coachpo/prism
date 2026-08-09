import { ExternalLink, KeyRound, Pencil, RotateCcw, Trash2 } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup";
import { Badge } from "@/components/ui/badge";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyApiKey } from "@/lib/types";
import { cn } from "@/lib/utils";
import { OperatorSectionCard } from "@/shared/design-system";
import {
  formatDateTime,
  formatLastUsed,
  getProxyKeyLifecycleLabel,
  getProxyKeyLifecycleTone,
  getProxyKeyLineageLabel,
} from "./proxyKeyFormatting";

interface ProxyKeyLedgerCardProps {
  authEnabled: boolean;
  deletingProxyKeyId: number | null;
  displayedProxyKeys: ProxyApiKey[];
  onDelete: (item: ProxyApiKey) => void;
  onEdit: (item: ProxyApiKey) => void;
  onRotate: (keyId: number) => void;
  proxyKeySuccessorByParentId: Map<number, number>;
  rotatingProxyKeyId: number | null;
}

type LedgerRowProps = {
  authEnabled: boolean;
  deleting: boolean;
  item: ProxyApiKey;
  onDelete: () => void;
  onEdit: () => void;
  onRotate: () => void;
  rotating: boolean;
  successorId: number | null;
};

type ProxyKeyActionsProps = Pick<LedgerRowProps, "deleting" | "onDelete" | "onEdit" | "onRotate" | "rotating"> & {
  item: ProxyApiKey;
  itemName: string;
};

function MobileOnlyLabel({ children }: { children: string }) {
  return <span className="text-[10px] font-semibold uppercase tracking-[0.18em] text-muted-foreground md:hidden">{children}</span>;
}

function MobileField({
  label,
  value,
  mono = false,
  className,
}: {
  label: string;
  value: string;
  mono?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1 bg-transparent px-0 py-0 md:gap-0", className)}>
      <MobileOnlyLabel>{label}</MobileOnlyLabel>
      <p className={cn("text-xs text-foreground", mono ? "break-all font-mono" : undefined)}>{value}</p>
    </div>
  );
}

function ProxyKeyActions({ deleting, item, itemName, onDelete, onEdit, onRotate, rotating }: ProxyKeyActionsProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;

  return (
    <IconActionGroup>
      <IconActionButton type="button" size="icon-sm" asChild aria-label={copy.viewRequestsAria(itemName)} disabled={rotating || deleting}>
        <Link to="/observe/requests" search={{ proxy_api_key_id: String(item.id), time_range: "7d" }}>
          <ExternalLink />
        </Link>
      </IconActionButton>
      <IconActionButton
        type="button"
        size="icon-sm"
        aria-label={copy.editProxyKeyAria(itemName)}
        disabled={rotating || deleting}
        onClick={onEdit}
      >
        <Pencil />
      </IconActionButton>
      <IconActionButton
        type="button"
        size="icon-sm"
        aria-label={copy.rotateProxyKeyAria(itemName)}
        disabled={rotating || deleting}
        onClick={onRotate}
      >
        <RotateCcw className={cn(rotating ? "animate-spin" : undefined)} />
      </IconActionButton>
      <IconActionButton
        type="button"
        size="icon-sm"
        aria-label={copy.deleteProxyKeyAria(itemName)}
        destructive
        disabled={rotating || deleting}
        onClick={onDelete}
      >
        <Trash2 />
      </IconActionButton>
    </IconActionGroup>
  );
}

function ProxyKeyLedgerRow({
  item,
  authEnabled,
  rotating,
  deleting,
  onEdit,
  onRotate,
  onDelete,
  successorId,
}: LedgerRowProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const statusLabel = getProxyKeyLifecycleLabel(item, authEnabled, successorId);
  const statusTone = getProxyKeyLifecycleTone(item, authEnabled, successorId);
  const note = item.notes?.trim() || copy.noInternalNote;
  const expiresAt = item.expires_at ? formatDateTime(item.expires_at, copy.neverExpires) : copy.neverExpires;
  const lineage = getProxyKeyLineageLabel(item, successorId);
  const lastIp = item.last_used_ip || "-";

  return (
    <TableRow className="grid gap-3 px-3 py-3 md:table-row md:px-0 md:py-0">
      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)]">
        <div className="flex min-w-0 flex-col gap-1">
          <MobileOnlyLabel>{copy.nameNote}</MobileOnlyLabel>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="truncate font-medium" title={item.name}>
              {item.name}
            </span>
            <Badge variant="outline" className={statusTone}>
              {statusLabel}
            </Badge>
          </div>
          <p className="truncate text-xs text-muted-foreground" title={note}>
            {note}
          </p>
        </div>
      </TableCell>

      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:max-w-[14rem] md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)]">
        <div className="flex flex-col gap-1">
          <MobileOnlyLabel>{copy.preview}</MobileOnlyLabel>
          <p className="break-all font-mono text-xs text-muted-foreground">{item.key_preview}</p>
        </div>
      </TableCell>

      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)]">
        <MobileField label={copy.created} value={formatDateTime(item.created_at)} />
      </TableCell>

      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)]">
        <MobileField label={copy.updated} value={formatDateTime(item.updated_at)} />
      </TableCell>

      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)]">
        <MobileField label={copy.expiresAt} value={expiresAt} />
      </TableCell>

      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)]">
        <MobileField label={copy.lineage} value={lineage} mono />
      </TableCell>

      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)]">
        <MobileField label={copy.lastUsed} value={formatLastUsed(item.last_used_at)} />
      </TableCell>

      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:max-w-[12rem] md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)]">
        <MobileField label={copy.lastIp} value={lastIp} mono />
      </TableCell>

      <TableCell className="block whitespace-normal px-0 py-0 align-top md:table-cell md:px-[var(--density-table-cell-px)] md:py-[var(--density-table-cell-py)] md:text-right">
        <div className="flex items-center justify-between gap-3 md:justify-end">
          <MobileOnlyLabel>{copy.operation}</MobileOnlyLabel>
          <ProxyKeyActions
            item={item}
            itemName={item.name}
            rotating={rotating}
            deleting={deleting}
            onEdit={onEdit}
            onRotate={onRotate}
            onDelete={onDelete}
          />
        </div>
      </TableCell>
    </TableRow>
  );
}

export function ProxyKeyLedgerCard({
  authEnabled,
  deletingProxyKeyId,
  displayedProxyKeys,
  onDelete,
  onEdit,
  onRotate,
  proxyKeySuccessorByParentId,
  rotatingProxyKeyId,
}: ProxyKeyLedgerCardProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.proxyApiKeys;

  return (
    <OperatorSectionCard
      className="overflow-hidden"
      title={copy.issuedKeys}
      description={copy.listDescription}
      actions={(
          <Badge variant="outline">{copy.keyCount(formatNumber(displayedProxyKeys.length))}</Badge>
      )}
    >
        <details className="rounded-lg border border-outline-variant bg-surface-container-low px-3 py-2">
          <summary className="cursor-pointer text-sm font-medium text-foreground select-none">
            {copy.lifecycleComparisonTitle}
          </summary>
          <div className="mt-2 overflow-x-auto">
            <table className="w-full min-w-[36rem] text-left text-xs">
              <thead>
                <tr className="border-b border-outline-variant text-muted-foreground">
                  <th className="py-1.5 pr-3 font-medium">{copy.lifecycleAction}</th>
                  <th className="py-1.5 pr-3 font-medium">{copy.lifecycleOldCredential}</th>
                  <th className="py-1.5 pr-3 font-medium">{copy.lifecycleHistory}</th>
                  <th className="py-1.5 font-medium">{copy.lifecycleWhen}</th>
                </tr>
              </thead>
              <tbody>
                <tr className="border-b border-outline-variant/60">
                  <td className="py-1.5 pr-3 font-medium">{copy.retireDescription}</td>
                  <td className="py-1.5 pr-3 text-muted-foreground">{copy.lifecycleRetireCredential}</td>
                  <td className="py-1.5 pr-3 text-muted-foreground">{copy.lifecycleHistoryKept}</td>
                  <td className="py-1.5 text-muted-foreground">{copy.lifecycleRetireWhen}</td>
                </tr>
                <tr className="border-b border-outline-variant/60">
                  <td className="py-1.5 pr-3 font-medium">{copy.rotated}</td>
                  <td className="py-1.5 pr-3 text-muted-foreground">{copy.lifecycleRotateCredential}</td>
                  <td className="py-1.5 pr-3 text-muted-foreground">{copy.lifecycleHistoryKept}</td>
                  <td className="py-1.5 text-muted-foreground">{copy.lifecycleRotateWhen}</td>
                </tr>
                <tr>
                  <td className="py-1.5 pr-3 font-medium">{copy.deleteKey}</td>
                  <td className="py-1.5 pr-3 text-muted-foreground">{copy.lifecycleDeleteCredential}</td>
                  <td className="py-1.5 pr-3 text-muted-foreground">{copy.lifecycleDeleteHistory}</td>
                  <td className="py-1.5 text-muted-foreground">{copy.lifecycleDeleteWhen}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </details>

        {displayedProxyKeys.length === 0 ? (
          <Empty className="border border-outline-variant bg-surface-container-low">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <KeyRound />
              </EmptyMedia>
              <EmptyTitle>{copy.noProxyKeysCreated}</EmptyTitle>
              <EmptyDescription>{copy.noProxyKeysDescription}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="rounded-lg border">
            <Table>
              <TableHeader className="hidden md:table-header-group">
                <TableRow>
                  <TableHead>{copy.nameNote}</TableHead>
                  <TableHead>{copy.preview}</TableHead>
                  <TableHead>{messages.loadbalanceEvents.created}</TableHead>
                  <TableHead>{copy.updated}</TableHead>
                  <TableHead>{copy.expiresAt}</TableHead>
                  <TableHead>{copy.lineage}</TableHead>
                  <TableHead>{copy.lastUsed}</TableHead>
                  <TableHead>{copy.lastIp}</TableHead>
                  <TableHead className="text-right">{copy.operation}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {displayedProxyKeys.map((item) => (
                  <ProxyKeyLedgerRow
                    key={item.id}
                    item={item}
                    authEnabled={authEnabled}
                    deleting={deletingProxyKeyId === item.id}
                    onDelete={() => onDelete(item)}
                    onEdit={() => onEdit(item)}
                    onRotate={() => onRotate(item.id)}
                    rotating={rotatingProxyKeyId === item.id}
                    successorId={proxyKeySuccessorByParentId.get(item.id) ?? null}
                  />
                ))}
              </TableBody>
            </Table>
          </div>
        )}
    </OperatorSectionCard>
  );
}
