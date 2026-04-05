import { ArrowLeft, Pencil } from "lucide-react";
import { CopyButton } from "@/components/CopyButton";
import { StatusBadge, TypeBadge } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import type { ModelConfig } from "@/lib/types";

interface ModelDetailHeaderProps {
  model: ModelConfig;
  onBack: () => void;
  onEditModel: () => void;
}

export function ModelDetailHeader({ model, onBack, onEditModel }: ModelDetailHeaderProps) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const typeLabel = model.model_type === "proxy" ? copy.typeProxy : copy.typeNative;
  const statusLabel = model.is_enabled ? copy.enabled : copy.disabled;

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
            <TypeBadge
              label={typeLabel}
              intent={model.model_type === "proxy" ? "accent" : "info"}
            />
            <StatusBadge
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
              {copy.modelConfigurationAndConnectionRouting}
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
