import { useLocale } from "@/i18n/useLocale";
import {
  OperatorInsetPanel,
  OperatorSectionCard,
} from "@/shared/design-system";
import { ModelsDevCatalogPanel } from "./ModelsDevCatalogPanel";
import { PiDevCatalogPanel } from "./PiDevCatalogPanel";
import type { ModelCatalogView } from "@/pages/model-detail/useModelCatalog";
import type {
  PiBindingController,
  PiCatalogModelView,
} from "@/features/models/catalog/pi/usePiBindingController";
import type { PiModelReadResponse } from "@/lib/types";

/**
 * The federated "外部目录来源" section on the model detail page: one inset
 * panel per source, each with its own loading/error/stale/empty surface and
 * its own actions. One source's failure never changes or masks the other
 * source, and neither one masks the model/target configuration above.
 *
 * The section shell owns only composition; each source panel owns its honest
 * read/action state while the pi.dev panel composes the shared binding
 * controller and dialogs.
 */
export function ExternalCatalogSourcesSection({
  modelConfigId,
  prismModelId,
  apiFamily,
  catalogView,
  piController,
  piRead,
  piReadFailed,
  piReadStale,
  piReadError,
  piLastSuccessfulAt,
  piActionsBlocked,
  piView,
  onPiRetry,
  piReadPending,
  piReadRefreshing,
  onCatalogChanged,
}: {
  modelConfigId: number;
  prismModelId: string;
  apiFamily: string;
  catalogView: ModelCatalogView;
  piController: PiBindingController;
  piRead: PiModelReadResponse | null;
  piReadFailed: boolean;
  piReadStale: boolean;
  piReadError: string | null;
  piLastSuccessfulAt: string | null;
  piActionsBlocked: boolean;
  piView: PiCatalogModelView | null;
  onPiRetry: () => void;
  piReadPending: boolean;
  piReadRefreshing: boolean;
  onCatalogChanged: () => void;
}) {
  const { messages } = useLocale();
  const copy = messages.externalCatalog;

  return (
    <OperatorSectionCard
      title={copy.sectionTitle}
      description={copy.sectionDescription}
    >
      <div className="flex flex-col gap-[var(--density-inline-gap)]">
        <ModelsDevCatalogPanel
          modelConfigId={modelConfigId}
          prismModelId={prismModelId}
          apiFamily={apiFamily}
          catalogView={catalogView}
          onChanged={onCatalogChanged}
        />
        <OperatorInsetPanel
          title={copy.piDevPanelTitle}
          description={copy.piDevPanelDescription}
        >
          <PiDevCatalogPanel
            controller={piController}
            read={piRead}
            readFailed={piReadFailed}
            readStale={piReadStale}
            readError={piReadError}
            lastSuccessfulAt={piLastSuccessfulAt}
            actionsBlocked={piActionsBlocked}
            view={piView}
            onRetry={onPiRetry}
            readPending={piReadPending}
            readRefreshing={piReadRefreshing}
          />
        </OperatorInsetPanel>
      </div>
    </OperatorSectionCard>
  );
}
