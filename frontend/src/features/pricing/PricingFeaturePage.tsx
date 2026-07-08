import { Upload } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useLocale } from "@/i18n/useLocale"
import { OperatorCallout, OperatorPageHeader, OperatorPageShell, OperatorTypeBadge } from "@/shared/design-system"
import { DeletePricingTemplateDialog } from "@/pages/pricing-templates/DeletePricingTemplateDialog"
import { PricingTemplateDialog } from "./PricingTemplateDialog"
import { PricingTemplateImportDialog } from "./PricingTemplateImportDialog"
import { PricingTemplatesTable } from "./PricingTemplatesTable"
import { PricingTemplateUsageDialog } from "@/pages/pricing-templates/PricingTemplateUsageDialog"
import { usePricingFeatureData } from "./usePricingFeatureData"

export function PricingFeaturePage() {
  const { messages } = useLocale()
  const copy = messages.pricingTemplatesUi
  const importCopy = messages.pricing
  const data = usePricingFeatureData(0)
  const selectedProfileLabel = "Default (#1)"

  return (
    <OperatorPageShell data-testid="pricing-feature-page">
      <OperatorPageHeader
        title={copy.title}
        description={copy.description}
        actions={<Button type="button" variant="outline" onClick={() => data.setPricingTemplateImportDialogOpen(true)}><Upload data-icon="inline-start" />{importCopy.importButton}</Button>}
      />
      <OperatorCallout
        action={<OperatorTypeBadge intent="warning" label={copy.profileScopedSettings} preserveLabel />}
        description={copy.scopeCallout(selectedProfileLabel)}
        intent="warning"
      />
      <PricingTemplatesTable onCreate={data.openCreatePricingTemplateDialog} onDelete={data.handleDeletePricingTemplateClick} onEdit={data.handleEditPricingTemplate} onViewUsage={data.handleViewPricingTemplateUsage} pricingTemplatePreparingEditId={data.pricingTemplatePreparingEditId} pricingTemplates={data.pricingTemplates} pricingTemplatesLoading={data.pricingTemplatesLoading} />
      <PricingTemplateDialog editingPricingTemplate={data.editingPricingTemplate} onClose={data.closePricingTemplateDialog} onOpenChange={data.setPricingTemplateDialogOpen} onSave={data.handleSavePricingTemplate} open={data.pricingTemplateDialogOpen} pricingTemplateSaving={data.pricingTemplateSaving} serverError={data.pricingTemplateServerError} />
      <PricingTemplateImportDialog importing={data.pricingTemplateImporting} onClose={() => data.setPricingTemplateImportDialogOpen(false)} onImport={data.handleImportPricingTemplates} onOpenChange={data.setPricingTemplateImportDialogOpen} open={data.pricingTemplateImportDialogOpen} />
      <PricingTemplateUsageDialog onOpenChange={data.setPricingTemplateUsageDialogOpen} open={data.pricingTemplateUsageDialogOpen} pricingTemplateUsageLoading={data.pricingTemplateUsageLoading} pricingTemplateUsageRows={data.pricingTemplateUsageRows} pricingTemplateUsageTemplate={data.pricingTemplateUsageTemplate} />
      <DeletePricingTemplateDialog deletePricingTemplateConfirm={data.deletePricingTemplateConfirm} displayTemplate={data.deletePricingTemplateDisplay ?? data.deletePricingTemplateConfirm} deletePricingTemplateConflict={data.deletePricingTemplateConflict} pricingTemplateUsageError={data.pricingTemplateUsageError} onClose={() => { data.setDeletePricingTemplateConfirm(null); data.setDeletePricingTemplateConflict(null) }} onDelete={data.handleDeletePricingTemplate} pricingTemplateDeleting={data.pricingTemplateDeleting} pricingTemplateUsageLoading={data.pricingTemplateUsageLoading} pricingTemplateUsageRows={data.pricingTemplateUsageRows} />
    </OperatorPageShell>
  )
}

export default PricingFeaturePage
