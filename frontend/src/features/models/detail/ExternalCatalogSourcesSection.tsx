import { useState } from "react";
import { ChevronRight } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
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
  // 目录证据是只读参考，却占了整页 40% 并把主配置面推到第三屏。
  // 默认折叠，展开状态按浏览器记住。
  const [expanded, setExpanded] = useState(readCatalogSectionExpanded);
  const toggle = () => {
    setExpanded((current) => {
      writeCatalogSectionExpanded(!current);
      return !current;
    });
  };

  return (
    <OperatorSectionCard
      title={copy.sectionTitle}
      description={copy.sectionDescription}
      actions={
        <Button type="button" size="sm" variant="ghost" onClick={toggle}>
          <ChevronRight
            data-icon="inline-start"
            className={cn("transition-transform", expanded && "rotate-90")}
          />
          {expanded ? copy.sectionCollapse : copy.sectionExpand}
        </Button>
      }
    >
      <div
        className="flex flex-col gap-[var(--density-inline-gap)]"
        hidden={!expanded}
      >
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

const CATALOG_SECTION_STORAGE_KEY = "prism.modelDetail.catalogExpanded";

function readCatalogSectionExpanded(): boolean {
  try {
    return localStorage.getItem(CATALOG_SECTION_STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

function writeCatalogSectionExpanded(expanded: boolean): void {
  try {
    localStorage.setItem(CATALOG_SECTION_STORAGE_KEY, expanded ? "1" : "0");
  } catch {
    // 存不下就算了：折叠状态只是便利，不影响页面正确性。
  }
}
