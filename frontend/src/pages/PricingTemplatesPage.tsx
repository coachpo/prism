import { useProfileContext } from "@/context/ProfileContext";
import { useLocale } from "@/i18n/useLocale";
import { PageHeader } from "@/components/PageHeader";
import { Badge } from "@/components/ui/badge";
import { DeletePricingTemplateDialog } from "./pricing-templates/DeletePricingTemplateDialog";
import { PricingTemplateDialog } from "./pricing-templates/PricingTemplateDialog";
import { PricingTemplatesTable } from "./pricing-templates/PricingTemplatesTable";
import { PricingTemplateUsageDialog } from "./pricing-templates/PricingTemplateUsageDialog";
import { usePricingTemplatesPageData } from "./pricing-templates/usePricingTemplatesPageData";

export function PricingTemplatesPage() {
  const { messages } = useLocale();
  const { selectedProfile, revision } = useProfileContext();
  const selectedProfileLabel = selectedProfile
    ? `${selectedProfile.name} (#${selectedProfile.id})`
    : messages.loadbalanceStrategiesPage.selectedProfileFallback;
  const data = usePricingTemplatesPageData(revision);
  const copy = messages.pricingTemplatesUi;

  return (
    <div className="space-y-6">
      <PageHeader
        title={copy.title}
        description={copy.description}
      />

      <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <Badge
            variant="outline"
            className="w-fit border-amber-500/30 bg-amber-500/15 text-amber-700 dark:text-amber-300"
          >
            {copy.profileScopedSettings}
          </Badge>
          <p className="text-sm text-amber-800 dark:text-amber-300">
            {copy.scopeCallout(selectedProfileLabel)}
          </p>
        </div>
      </div>

      <PricingTemplatesTable
        onCreate={data.openCreatePricingTemplateDialog}
        onDelete={data.handleDeletePricingTemplateClick}
        onEdit={data.handleEditPricingTemplate}
        onViewUsage={data.handleViewPricingTemplateUsage}
        pricingTemplatePreparingEditId={data.pricingTemplatePreparingEditId}
        pricingTemplates={data.pricingTemplates}
        pricingTemplatesLoading={data.pricingTemplatesLoading}
      />

      <PricingTemplateDialog
        editingPricingTemplate={data.editingPricingTemplate}
        onClose={data.closePricingTemplateDialog}
        onOpenChange={data.setPricingTemplateDialogOpen}
        onSave={data.handleSavePricingTemplate}
        open={data.pricingTemplateDialogOpen}
        pricingTemplateForm={data.pricingTemplateForm}
        pricingTemplateSaving={data.pricingTemplateSaving}
        setPricingTemplateForm={data.setPricingTemplateForm}
      />

      <PricingTemplateUsageDialog
        onOpenChange={data.setPricingTemplateUsageDialogOpen}
        open={data.pricingTemplateUsageDialogOpen}
        pricingTemplateUsageLoading={data.pricingTemplateUsageLoading}
        pricingTemplateUsageRows={data.pricingTemplateUsageRows}
        pricingTemplateUsageTemplate={data.pricingTemplateUsageTemplate}
      />

      <DeletePricingTemplateDialog
        deletePricingTemplateConfirm={data.deletePricingTemplateConfirm}
        deletePricingTemplateConflict={data.deletePricingTemplateConflict}
        onClose={() => {
          data.setDeletePricingTemplateConfirm(null);
          data.setDeletePricingTemplateConflict(null);
        }}
        onDelete={data.handleDeletePricingTemplate}
        pricingTemplateDeleting={data.pricingTemplateDeleting}
        pricingTemplateUsageLoading={data.pricingTemplateUsageLoading}
        pricingTemplateUsageRows={data.pricingTemplateUsageRows}
      />
    </div>
  );
}
