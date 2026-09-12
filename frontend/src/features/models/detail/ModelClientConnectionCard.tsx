import { CopyButton } from "@/components/CopyButton";
import { Button } from "@/components/ui/button";
import { getEffectiveBackendOrigin } from "@/features/runtime-self-test/effectiveOrigin";
import { useLocale } from "@/i18n/useLocale";
import type { ModelConfig } from "@/lib/types";
import { OperatorCallout, OperatorSectionCard } from "@/shared/design-system";

export function ModelClientConnectionCard({ model, onNavigateTo }: {
  model: ModelConfig;
  onNavigateTo: (to: string) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const origin = getEffectiveBackendOrigin().origin;
  const clientAddress = model.api_family === "openai" ? `${origin}/v1` : origin;
  const canRequest = model.is_enabled && model.direct_request_enabled === true;

  return (
    <OperatorSectionCard title={copy.clientConnectionTitle} description={copy.clientConnectionDescription} contentClassName="flex flex-col gap-3" data-testid="model-client-connection">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-xs text-muted-foreground">{copy.clientAddressLabel}</span>
          <div className="flex min-w-0 items-center gap-2">
            <span className="break-all font-mono text-sm">{clientAddress}</span>
            <CopyButton value={clientAddress} targetLabel={copy.clientAddressLabel} aria-label={copy.copyClientAddress} label="" size="icon-xs" variant="ghost" />
          </div>
        </div>
        <div className="flex min-w-0 flex-col gap-1">
          <span className="text-xs text-muted-foreground">{messages.modelsUi.modelIdLabel}</span>
          <div className="flex min-w-0 items-center gap-2">
            <span className="break-all font-mono text-sm">{model.model_id}</span>
            <CopyButton value={model.model_id} targetLabel={messages.modelsUi.modelIdLabel} aria-label={copy.copyModelIdAria()} label="" size="icon-xs" variant="ghost" />
          </div>
        </div>
      </div>
      <p className="text-xs text-muted-foreground">{copy.clientKeyHint}</p>
      {canRequest ? <p className="text-xs text-muted-foreground">{copy.clientVerificationHint}</p> : <OperatorCallout intent="warning" description={model.direct_request_enabled !== true ? copy.clientDirectDisabled : copy.clientModelDisabled} />}
      <div className="flex flex-wrap gap-2">
        <Button type="button" onClick={() => onNavigateTo("/system/proxy-keys")}>{copy.clientKeyAction}</Button>
        {canRequest ? <Button type="button" variant="outline" onClick={() => onNavigateTo(`/system/proxy-keys?action=verify&model_id=${encodeURIComponent(model.model_id)}`)}>{copy.clientVerifyAction}</Button> : null}
      </div>
    </OperatorSectionCard>
  );
}
