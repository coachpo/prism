import { useCallback, useState } from "react";
import { AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { OperatorCallout } from "@/shared/design-system";
import { api } from "@/lib/api";
import type { CostingSettingsUpdate, CurrencyMigrationPreview } from "@/lib/types";
import { getStaticMessages } from "@/i18n/staticMessages";
import { toast } from "sonner";

interface ArchiveUnusedFxDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentCosting: CostingSettingsUpdate;
  onArchived: () => Promise<void>;
}

export function ArchiveUnusedFxDialog({ open, onOpenChange, currentCosting, onArchived }: ArchiveUnusedFxDialogProps) {
  const copy = getStaticMessages().settingsCurrencyMigration;
  const [loading, setLoading] = useState(false);
  const [preview, setPreview] = useState<CurrencyMigrationPreview | null>(null);
  const [operationId, setOperationId] = useState<string | null>(null);
  const [verifiedCount, setVerifiedCount] = useState(0);

  const reset = useCallback(() => {
    setPreview(null);
    setOperationId(null);
    setVerifiedCount(0);
  }, []);

  const handlePreview = useCallback(async () => {
    const inventory = currentCosting.pricing_migration_inventory;
    const epoch = currentCosting.reporting_currency_epoch ? Number(currentCosting.reporting_currency_epoch) : NaN;
    if (!inventory?.archive_only_available || !Number.isSafeInteger(epoch) || epoch < 1 || !currentCosting.expected_updated_at) {
      toast.error(copy.previewFailed);
      return;
    }
    setLoading(true);
    try {
      const nextOperationId = crypto.randomUUID();
      const response = await api.settings.costing.currencyMigrationPreview({
        operation_kind: "archive_unused_fx",
        migration_operation_id: nextOperationId,
        draft_id: "",
        draft_hash: "",
        expected_inventory_id: inventory.inventory_id,
        expected_inventory_hash: inventory.inventory_hash,
        expected_inventory_generation: inventory.generation,
        expected_reporting_currency_epoch: epoch,
        expected_settings_updated_at: currentCosting.expected_updated_at,
      });
      const firstPage = response.fx_evidence_page;
      if (!firstPage) {
        throw new Error(copy.previewFailed);
      }
      if (firstPage.items.some((item) => item.attribution !== "unused" || item.dependency_count !== 0)) {
        throw new Error(copy.previewFailed);
      }
      let count = firstPage.items.length;
      let cursor = firstPage.next_cursor;
      const seen = new Set<string>();
      while (cursor) {
        if (seen.has(cursor)) {
          throw new Error(copy.previewFailed);
        }
        seen.add(cursor);
        const page = await api.settings.costing.currencyMigrationInventoryFXEvidence(inventory.inventory_id, { limit: 100, cursor });
        if (page.items.some((item) => item.attribution !== "unused" || item.dependency_count !== 0)) {
          throw new Error(copy.previewFailed);
        }
        count += page.items.length;
        cursor = page.next_cursor;
      }
      if (count !== firstPage.total_count) {
        throw new Error(copy.previewFailed);
      }
      setOperationId(nextOperationId);
      setVerifiedCount(count);
      setPreview(response);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : copy.previewFailed);
    } finally {
      setLoading(false);
    }
  }, [copy.previewFailed, currentCosting.expected_updated_at, currentCosting.pricing_migration_inventory, currentCosting.reporting_currency_epoch]);

  const handleCommit = useCallback(async () => {
    const inventory = currentCosting.pricing_migration_inventory;
    const epoch = currentCosting.reporting_currency_epoch ? Number(currentCosting.reporting_currency_epoch) : NaN;
    if (!preview || !operationId || !inventory || !Number.isSafeInteger(epoch) || !currentCosting.expected_updated_at) {
      return;
    }
    setLoading(true);
    try {
      await api.settings.costing.currencyMigrationCommit({
        operation_kind: "archive_unused_fx",
        migration_operation_id: operationId,
        draft_id: "",
        draft_hash: "",
        preview_hash: preview.preview_hash,
        expected_inventory_id: inventory.inventory_id,
        expected_inventory_hash: inventory.inventory_hash,
        expected_inventory_generation: inventory.generation,
        expected_reporting_currency_epoch: epoch,
        expected_settings_updated_at: currentCosting.expected_updated_at,
      });
      await onArchived();
      toast.success(copy.archiveSucceeded(verifiedCount));
      reset();
      onOpenChange(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : copy.commitFailed);
    } finally {
      setLoading(false);
    }
  }, [copy, currentCosting.expected_updated_at, currentCosting.pricing_migration_inventory, currentCosting.reporting_currency_epoch, onArchived, onOpenChange, operationId, preview, reset, verifiedCount]);

  const handleOpenChange = useCallback((next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  }, [onOpenChange, reset]);

  return (
    <AlertDialog open={open} onOpenChange={handleOpenChange}>
      <AlertDialogContent aria-describedby="archive-fx-description">
        <AlertDialogHeader>
          <AlertDialogTitle>{copy.archiveButton}</AlertDialogTitle>
          <AlertDialogDescription id="archive-fx-description">{copy.archiveDescription}</AlertDialogDescription>
        </AlertDialogHeader>
        {!preview ? (
          <OperatorCallout intent="warning" description={copy.archiveDescription} />
        ) : (
          <OperatorCallout intent="success" description={copy.archiveSummary(verifiedCount, preview.fx_evidence_page?.total_count ?? verifiedCount)} />
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={loading}>{copy.cancel}</AlertDialogCancel>
          {!preview ? (
            <Button type="button" onClick={() => void handlePreview()} disabled={loading}>
              {loading ? copy.previewing : copy.archivePreview}
            </Button>
          ) : (
            <Button type="button" variant="destructive" onClick={() => void handleCommit()} disabled={loading}>
              {loading ? copy.committing : copy.archiveCommit}
            </Button>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
