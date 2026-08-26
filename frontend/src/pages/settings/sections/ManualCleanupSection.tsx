import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import { OperatorSectionCard, OperatorTypeBadge } from "@/shared/design-system";
import type { CleanupType, RetentionPreset } from "../manualCleanup";

interface ManualCleanupSectionProps {
  cleanupType: CleanupType;
  deleting: boolean;
  handleOpenDeleteConfirm: () => Promise<void>;
  preflightLoading: boolean;
  retentionPreset: RetentionPreset;
  setCleanupType: (value: CleanupType) => void;
  setRetentionPreset: (value: RetentionPreset) => void;
}

/**
 * Manual cleanup is not a footnote on the retention policy card: it deletes
 * data now, irreversibly. It gets its own danger-outlined card so the left
 * table of contents and the cards on the page line up one to one, and the
 * button names the real flow — preflight first, then a typed confirmation.
 */
export function ManualCleanupSection({
  cleanupType,
  deleting,
  handleOpenDeleteConfirm,
  preflightLoading,
  retentionPreset,
  setCleanupType,
  setRetentionPreset,
}: ManualCleanupSectionProps) {
  const { messages } = useLocale();
  const copy = messages.settingsRetentionDeletion;
  const dialogCopy = messages.settingsDialogs;

  return (
    <section id="manual-cleanup" tabIndex={-1} className="scroll-mt-24">
      <OperatorSectionCard
        className="border-destructive/40"
        data-testid="manual-cleanup-section"
        title={copy.manualCleanupTitle}
        description={copy.manualCleanupDescription}
        actions={
          <OperatorTypeBadge intent="danger" label={messages.honesty.irreversible} preserveLabel />
        }
        contentClassName="flex flex-col gap-3"
      >
        <p className="text-xs text-muted-foreground">{copy.manualCleanupPreamble}</p>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field>
            <FieldLabel>{copy.dataType}</FieldLabel>
            <Select value={cleanupType} onValueChange={(value) => setCleanupType(value as CleanupType)}>
              <SelectTrigger>
                <SelectValue placeholder={copy.selectDataType} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="requests">{dialogCopy.cleanupTypeRequests}</SelectItem>
                  <SelectItem value="statistics">{dialogCopy.cleanupTypeStatistics}</SelectItem>
                  <SelectItem value="audits">{dialogCopy.cleanupTypeAudits}</SelectItem>
                  <SelectItem value="loadbalance_events">{dialogCopy.cleanupTypeLoadbalanceEvents}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel>{copy.deleteOlderThan}</FieldLabel>
            <Select value={retentionPreset} onValueChange={(value) => setRetentionPreset(value as RetentionPreset)}>
              <SelectTrigger>
                <SelectValue placeholder={copy.selectRetention} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="1">{copy.retentionDays(1)}</SelectItem>
                  <SelectItem value="7">{copy.retentionDays(7)}</SelectItem>
                  <SelectItem value="30">{copy.retentionDays(30)}</SelectItem>
                  <SelectItem value="90">{copy.retentionDays(90)}</SelectItem>
                  <SelectItem value="all" className="text-destructive">
                    {copy.allData}
                  </SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
          <Field className="justify-end">
            <Button
              type="button"
              variant="destructive"
              className="w-full"
              disabled={deleting || preflightLoading || !cleanupType || !retentionPreset}
              onClick={() => void handleOpenDeleteConfirm()}
            >
              {preflightLoading ? copy.previewing : copy.preflightAndDelete}
            </Button>
            {!cleanupType || !retentionPreset ? (
              <FieldDescription>{copy.manualCleanupDisabledReason}</FieldDescription>
            ) : null}
          </Field>
        </div>
      </OperatorSectionCard>
    </section>
  );
}
