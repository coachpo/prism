import { useProfileContext } from "@/context/ProfileContext"
import { useLocale } from "@/i18n/useLocale"
import { OperatorCallout, OperatorPageHeader, OperatorPageShell, OperatorTypeBadge } from "@/shared/design-system"
import { DeletePricingTemplateDialog } from "@/pages/pricing-templates/DeletePricingTemplateDialog"
import { PricingTemplateDialog } from "./PricingTemplateDialog"
import { PricingTemplatesTable } from "./PricingTemplatesTable"
import { PricingTemplateUsageDialog } from "@/pages/pricing-templates/PricingTemplateUsageDialog"
import { usePricingFeatureData } from "./usePricingFeatureData"

export function PricingFeaturePage() {
  const { messages } = useLocale()
  const { selectedProfile, revision } = useProfileContext()
  const copy = messages.pricingTemplatesUi
  const data = usePricingFeatureData(revision)
  const selectedProfileLabel = selectedProfile ? `${selectedProfile.name} (#${selectedProfile.id})` : copy.selectedProfileFallback

  return (
    <OperatorPageShell data-testid="pricing-feature-page">
      <OperatorPageHeader title={copy.title} description={copy.description} />
      <OperatorCallout
        action={<OperatorTypeBadge intent="warning" label={copy.profileScopedSettings} preserveLabel />}
        description={copy.scopeCallout(selectedProfileLabel)}
        intent="warning"
      />
      <PricingTemplatesTable onCreate={data.openCreatePricingTemplateDialog} onDelete={data.handleDeletePricingTemplateClick} onEdit={data.handleEditPricingTemplate} onViewUsage={data.handleViewPricingTemplateUsage} pricingTemplatePreparingEditId={data.pricingTemplatePreparingEditId} pricingTemplates={data.pricingTemplates} pricingTemplatesLoading={data.pricingTemplatesLoading} />
      <PricingTemplateDialog editingPricingTemplate={data.editingPricingTemplate} onClose={data.closePricingTemplateDialog} onOpenChange={data.setPricingTemplateDialogOpen} onSave={data.handleSavePricingTemplate} open={data.pricingTemplateDialogOpen} pricingTemplateSaving={data.pricingTemplateSaving} serverError={data.pricingTemplateServerError} />
      <PricingTemplateUsageDialog onOpenChange={data.setPricingTemplateUsageDialogOpen} open={data.pricingTemplateUsageDialogOpen} pricingTemplateUsageLoading={data.pricingTemplateUsageLoading} pricingTemplateUsageRows={data.pricingTemplateUsageRows} pricingTemplateUsageTemplate={data.pricingTemplateUsageTemplate} />
      <DeletePricingTemplateDialog deletePricingTemplateConfirm={data.deletePricingTemplateConfirm} displayTemplate={data.deletePricingTemplateDisplay ?? data.deletePricingTemplateConfirm} deletePricingTemplateConflict={data.deletePricingTemplateConflict} pricingTemplateUsageError={data.pricingTemplateUsageError} onClose={() => { data.setDeletePricingTemplateConfirm(null); data.setDeletePricingTemplateConflict(null) }} onDelete={data.handleDeletePricingTemplate} pricingTemplateDeleting={data.pricingTemplateDeleting} pricingTemplateUsageLoading={data.pricingTemplateUsageLoading} pricingTemplateUsageRows={data.pricingTemplateUsageRows} />
    </OperatorPageShell>
  )
}

export default PricingFeaturePage
