import { ArrowLeft, Pencil } from "lucide-react";
import { CopyButton } from "@/components/CopyButton";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import type { ModelConfig } from "@/lib/types";
import { OperatorStatusBadge } from "@/shared/design-system";
import { buildAccessTargetSummary } from "./useModelDetailDataSupport";

interface ModelDetailHeaderProps {
  model: ModelConfig;
  onBack: () => void;
  onEditModel: () => void;
}

export function ModelDetailHeader({ model, onBack, onEditModel }: ModelDetailHeaderProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.modelDetail;
  const modelsUiCopy = messages.modelsUi;
  const statusLabel = model.is_enabled ? copy.enabled : copy.disabled;
  const accessTargetSummary = buildAccessTargetSummary(model);
  const accessTargetSegments = accessTargetSummary.enabledTargetCount > 0
    ? [
        accessTargetSummary.enabledModelFallbackTargetCount > 0
          ? `${modelsUiCopy.modelFallbackTargets}: ${formatNumber(accessTargetSummary.enabledModelFallbackTargetCount)}`
          : null,
        accessTargetSummary.enabledTerminalTargetCount > 0
          ? `${modelsUiCopy.terminalTargets}: ${formatNumber(accessTargetSummary.enabledTerminalTargetCount)}`
          : null,
      ].filter((segment): segment is string => segment !== null)
    : [];
  const accessTargetLabel = accessTargetSegments.length > 0
    ? accessTargetSegments.join(" · ")
    : accessTargetSummary.totalTargetCount > 0
      ? `${modelsUiCopy.accessTargets}: ${formatNumber(accessTargetSummary.totalTargetCount)} · ${copy.disabled}`
      : `${modelsUiCopy.accessTargets}: ${formatNumber(0)}`;

  return (
    <div className="rounded-2xl border bg-card p-4 sm:p-5">
      <div className="relative flex items-center gap-3">
        <Button
          variant="ghost"
          size="icon"
          className="h-9 w-9 shrink-0 rounded-md"
          aria-label={copy.backToModels}
          onClick={onBack}
        >
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 flex-wrap">
            <h1 className="text-xl font-semibold tracking-tight truncate">
              {model.display_name || model.model_id}
            </h1>
            {!model.display_name ? (
              <CopyButton
                value={model.model_id}
                label=""
                targetLabel={copy.modelIdLabel}
                aria-label={copy.copyModelIdAria(model.model_id)}
                variant="ghost"
                size="icon-xs"
                className="h-7 w-7 shrink-0 rounded-full text-muted-foreground hover:text-foreground"
              />
            ) : null}
            <OperatorStatusBadge label={accessTargetLabel} intent="info" />
            <OperatorStatusBadge
              label={statusLabel}
              intent={model.is_enabled ? "success" : "muted"}
            />
          </div>
          {model.display_name ? (
            <div className="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
              <span className="font-mono">{model.model_id}</span>
              <CopyButton
                value={model.model_id}
                label=""
                targetLabel={copy.modelIdLabel}
                aria-label={copy.copyModelIdAria(model.model_id)}
                variant="ghost"
                size="icon-xs"
                className="h-7 w-7 shrink-0 rounded-full text-muted-foreground hover:text-foreground"
              />
            </div>
          ) : (
            <p className="mt-1 text-xs text-muted-foreground">
              {copy.modelRoutingAccessTargetsAndTerminalTargets}
            </p>
          )}
        </div>

        <Button
          variant="outline"
          size="icon"
          className="h-9 w-9 shrink-0"
          aria-label={copy.editModel}
          onClick={onEditModel}
        >
          <Pencil className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  );
}
