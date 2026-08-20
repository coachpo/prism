import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import type { PricingTemplateRevision } from "@/lib/types";
import { OperatorInsetPanel, OperatorMissingValue, OperatorValueBadge } from "@/shared/design-system";

function revisionStructure(revision: PricingTemplateRevision) {
  return JSON.stringify({ kind: revision.template_kind, card: revision.card, base: revision.base_card, tier: revision.tier, peak: revision.peak_card, offpeak: revision.offpeak_card, schedule: revision.schedule });
}

export function PricingTemplateHistoryPanel({ loading, revisions }: { loading: boolean; revisions: PricingTemplateRevision[] }) {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.pricingTemplatesUi;
  const history = messages.pricingTemplatesHistory;
  if (loading) return <Skeleton className="h-20 rounded-md" />;
  if (revisions.length === 0) return <OperatorInsetPanel><p className="text-xs text-muted-foreground">{history.empty}</p></OperatorInsetPanel>;
  return (
    <OperatorInsetPanel className="p-0">
      <div className="overflow-x-auto">
        <Table>
          <TableHeader><TableRow><TableHead>{copy.columnVersion}</TableHead><TableHead>{history.tableKind}</TableHead><TableHead>{history.tableCards}</TableHead><TableHead>{history.effectiveAt}</TableHead><TableHead>{history.createdBy}</TableHead></TableRow></TableHeader>
          <TableBody>
            {revisions.map((revision, index) => {
              const previous = revisions[index + 1];
              const changed = Boolean(previous && revisionStructure(revision) !== revisionStructure(previous));
              const cardCount = [revision.card, revision.base_card, revision.tier?.card, revision.peak_card, revision.offpeak_card].filter(Boolean).length;
              const kind = revision.template_kind === "standard" ? copy.kindStandard : revision.template_kind === "tiered" ? copy.kindTiered : copy.kindPeakValley;
              return (
                <TableRow key={revision.revision_id}>
                  <TableCell><OperatorValueBadge label={`v${revision.version}`} className="text-xs" /></TableCell>
                  <TableCell><OperatorValueBadge label={kind} className="text-xs" /></TableCell>
                  <TableCell className="text-xs">{copy.multiCardSummary(cardCount)}{changed ? <span className="ml-2 text-failing">{history.structureChanged}</span> : null}</TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">{revision.effective_at ? formatTime(revision.effective_at) : <OperatorMissingValue className="text-xs" />}</TableCell>
                  <TableCell className="text-xs">{history.createdByKind(revision.created_by_kind)}</TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </OperatorInsetPanel>
  );
}
