import { EmptyState } from "@/components/EmptyState";
import { useLocale } from "@/i18n/useLocale";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { ProxyApiKey } from "@/lib/types";
import { ProxyKeyCard } from "./ProxyKeyCard";

interface ProxyKeysListCardProps {
  authEnabled: boolean;
  deletingProxyKeyId: number | null;
  displayedProxyKeys: ProxyApiKey[];
  onDelete: (item: ProxyApiKey) => void;
  onEdit: (item: ProxyApiKey) => void;
  onRotate: (keyId: number) => void;
  rotatingProxyKeyId: number | null;
}

export function ProxyKeysListCard({
  authEnabled,
  deletingProxyKeyId,
  displayedProxyKeys,
  onDelete,
  onEdit,
  onRotate,
  rotatingProxyKeyId,
}: ProxyKeysListCardProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.proxyApiKeys;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="text-base">{copy.issuedKeys}</CardTitle>
            <p className="text-sm text-muted-foreground">
              {copy.listDescription}
            </p>
          </div>
          <p className="text-sm text-muted-foreground">
            {copy.keyCount(formatNumber(displayedProxyKeys.length))}
          </p>
        </div>
      </CardHeader>
      <CardContent>
        {displayedProxyKeys.length === 0 ? (
          <EmptyState
            title={copy.noProxyKeysCreated}
            className="rounded-md border border-dashed px-3 py-6 [&>div:first-child]:hidden"
          />
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader className="hidden md:table-header-group">
                <TableRow>
                  <TableHead>{copy.nameNote}</TableHead>
                  <TableHead>{copy.preview}</TableHead>
                  <TableHead>{messages.loadbalanceEvents.created}</TableHead>
                  <TableHead>{copy.updated}</TableHead>
                  <TableHead>{copy.lastUsed}</TableHead>
                  <TableHead>{copy.lastIp}</TableHead>
                  <TableHead className="text-right">{copy.operation}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {displayedProxyKeys.map((item) => {
                  const rotating = rotatingProxyKeyId === item.id;
                  const deleting = deletingProxyKeyId === item.id;

                  return (
                    <ProxyKeyCard
                      key={item.id}
                      item={item}
                      authEnabled={authEnabled}
                      rotating={rotating}
                      deleting={deleting}
                      onEdit={() => onEdit(item)}
                      onRotate={() => onRotate(item.id)}
                      onDelete={() => onDelete(item)}
                    />
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
