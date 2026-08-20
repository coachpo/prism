import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useLocale } from "@/i18n/useLocale"
import type { PricingTemplateImportRequest, PricingTemplateImportResponse } from "@/lib/types"
import { OperatorMissingValue, OperatorSectionCard, OperatorTypeBadge } from "@/shared/design-system"

export type PricingImportPreviewState = {
  request: PricingTemplateImportRequest
  response: PricingTemplateImportResponse
}

interface PricingImportPreviewProps {
  committing: boolean
  onCancel: () => void
  onCommit: () => void
  preview: PricingImportPreviewState
}

/**
 * The preview half of the two-phase import, on the page instead of buried in
 * a toast. The commit is a separate, explicit act: nothing is written until
 * the operator has seen which templates would be created, updated or skipped
 * and at what version.
 */
export function PricingImportPreview({ committing, onCancel, onCommit, preview }: PricingImportPreviewProps) {
  const { messages } = useLocale()
  const copy = messages.pricingTemplatesUi
  const items = preview.response.items ?? []
  const blocked = preview.response.errors.length > 0 || preview.response.committable === false

  // The table runs to the card edge — the card border is its border — so the
  // content area drops its gutter and the non-table blocks carry their own.
  return (
    <OperatorSectionCard
      className="gap-0 border-degraded/30"
      title={copy.importPreviewTitle}
      description={copy.importPreviewDescription}
      data-testid="pricing-import-preview"
      contentClassName="flex flex-col gap-3 px-0"
      actions={
        <div className="flex items-center gap-2">
          <Button type="button" variant="ghost" size="sm" onClick={onCancel} disabled={committing}>
            {copy.importPreviewCancel}
          </Button>
          <Button type="button" size="sm" onClick={onCommit} disabled={committing || blocked}>
            {committing ? <Spinner aria-hidden="true" data-icon="inline-start" /> : null}
            {copy.importPreviewCommit}
          </Button>
        </div>
      }
    >
      {blocked ? (
        <p className="px-[var(--density-card-pad-x)] text-xs text-destructive" role="alert">
          {preview.response.errors[0]?.detail ?? copy.importPreviewBlocked}
        </p>
      ) : null}

      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{copy.importPreviewName}</TableHead>
              <TableHead>{copy.importPreviewAction}</TableHead>
              <TableHead>{copy.importPreviewVersion}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={`${item.name}-${item.action}`}>
                <TableCell className="text-sm font-medium">{item.name}</TableCell>
                <TableCell>
                  <div className="flex flex-col gap-1">
                    <OperatorTypeBadge
                      preserveLabel
                      intent={item.action === "create" ? "healthy" : item.action === "update" ? "accent" : "muted"}
                      label={
                        item.action === "create"
                          ? copy.importPreviewCreate
                          : item.action === "update"
                            ? copy.importPreviewUpdate
                            : copy.importPreviewSkip
                      }
                    />
                    {item.template_kind_changed ? (
                      <span className="text-[11px] text-muted-foreground">{copy.importPreviewKindChanged}</span>
                    ) : item.pricing_structure_changed ? (
                      <span className="text-[11px] text-muted-foreground">{copy.importPreviewStructureChanged}</span>
                    ) : item.action === "update" ? (
                      <span className="text-[11px] text-muted-foreground">{copy.importPreviewMetadataOnly}</span>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell className="font-mono text-xs tabular-nums">
                  {item.current_version != null && item.next_version != null ? (
                    copy.importPreviewVersionChange(String(item.current_version), String(item.next_version))
                  ) : item.next_version != null ? (
                    `v${item.next_version}`
                  ) : (
                    <OperatorMissingValue className="text-xs" />
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </OperatorSectionCard>
  )
}
