import { useProfileContext } from "@/context/ProfileContext";
import { useLocale } from "@/i18n/useLocale";
import { PageHeader } from "@/components/PageHeader";
import { Badge } from "@/components/ui/badge";
import { DeleteLoadbalanceStrategyDialog } from "./loadbalance-strategies/DeleteLoadbalanceStrategyDialog";
import { LoadbalanceStrategiesTable } from "./loadbalance-strategies/LoadbalanceStrategiesTable";
import { LoadbalanceStrategyDialog } from "./loadbalance-strategies/LoadbalanceStrategyDialog";
import { useLoadbalanceStrategiesPageData } from "./loadbalance-strategies/useLoadbalanceStrategiesPageData";

export function LoadbalanceStrategiesPage() {
  const { messages } = useLocale();
  const { selectedProfile, revision } = useProfileContext();
  const selectedProfileLabel = selectedProfile
    ? `${selectedProfile.name} (#${selectedProfile.id})`
    : messages.loadbalanceStrategiesPage.selectedProfileFallback;
  const pageCopy = messages.loadbalanceStrategiesPage;
  const data = useLoadbalanceStrategiesPageData(revision);

  return (
    <div className="space-y-6">
      <PageHeader
        title={messages.nav.loadbalanceStrategies}
        description={pageCopy.description}
      />

      <div className="rounded-lg border border-amber-500/25 bg-amber-500/10 p-4">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <Badge
            variant="outline"
            className="w-fit border-amber-500/30 bg-amber-500/15 text-amber-700 dark:text-amber-300"
          >
            {messages.settingsPage.profileScopedSettings}
          </Badge>
          <p className="text-sm text-amber-800 dark:text-amber-300">
            {pageCopy.scopeCallout(selectedProfileLabel)}
          </p>
        </div>
      </div>

      <LoadbalanceStrategiesTable
        loadbalanceStrategies={data.loadbalanceStrategies}
        loadbalanceStrategiesLoading={data.loadbalanceStrategiesLoading}
        loadbalanceStrategyPreparingEditId={data.loadbalanceStrategyPreparingEditId}
        onCreate={data.openCreateLoadbalanceStrategyDialog}
        onDelete={data.handleDeleteLoadbalanceStrategyClick}
        onEdit={data.handleEditLoadbalanceStrategy}
      />

      <LoadbalanceStrategyDialog
        editingLoadbalanceStrategy={data.editingLoadbalanceStrategy}
        loadbalanceStrategyForm={data.loadbalanceStrategyForm}
        loadbalanceStrategySaving={data.loadbalanceStrategySaving}
        onClose={data.closeLoadbalanceStrategyDialog}
        onOpenChange={data.setLoadbalanceStrategyDialogOpen}
        onSave={data.handleSaveLoadbalanceStrategy}
        open={data.loadbalanceStrategyDialogOpen}
        setLoadbalanceStrategyForm={data.setLoadbalanceStrategyForm}
      />

      <DeleteLoadbalanceStrategyDialog
        deleteLoadbalanceStrategyConfirm={data.deleteLoadbalanceStrategyConfirm}
        displayedDeleteLoadbalanceStrategyConfirm={data.displayedDeleteLoadbalanceStrategyConfirm}
        loadbalanceStrategyDeleting={data.loadbalanceStrategyDeleting}
        onClose={data.closeDeleteLoadbalanceStrategyDialog}
        onDelete={data.handleDeleteLoadbalanceStrategy}
        open={data.deleteLoadbalanceStrategyDialogOpen}
      />
    </div>
  );
}
