import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import type { ModelConfigListItem } from "@/lib/types";
import { OperatorCallout, OperatorErrorState, OperatorRetryButton } from "@/shared/design-system";
import { buildSelfTestCurl } from "@/features/runtime-self-test/curlBuilder";
import { RuntimeSelfTestPanel } from "@/features/runtime-self-test/RuntimeSelfTestPanel";
import { Link } from "@tanstack/react-router";
import type { SelfTestRequestSpec } from "@/features/runtime-self-test/selfTestTypes";
import { runtimeSelfTestModelCandidates } from "@/features/runtime-self-test/modelCandidates";

interface ProxyKeyVerifyAccessDialogProps {
  models: ModelConfigListItem[];
  authEnabled?: boolean;
  initialModelId?: string;
  modelsError: boolean;
  modelsLoading: boolean;
  onOpenChange: (open: boolean) => void;
  onRetryModels: () => void;
  open: boolean;
}

/** Credentials are used only by the runtime request and cleared on close. */
export function ProxyKeyVerifyAccessDialog({
  models,
  authEnabled,
  initialModelId,
  modelsError,
  modelsLoading,
  onOpenChange,
  onRetryModels,
  open,
}: ProxyKeyVerifyAccessDialogProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const [rawKey, setRawKey] = useState("");
  const [noKeyChoice, setNoKeyChoice] = useState<boolean | null>(null);
  const noKey = authEnabled === false && (noKeyChoice ?? true);
  const [selectedModelId, setSelectedModelId] = useState(initialModelId ?? "");
  const [operation, setOperation] = useState<"responses" | "chat_completions">("responses");
  const [busy, setBusy] = useState(false);
  const [inputRevision, setInputRevision] = useState(0);
  const resetResult = () => setInputRevision((revision) => revision + 1);

  const candidates = useMemo(() => runtimeSelfTestModelCandidates(models), [models]);
  const selectedModel = useMemo(
    () => candidates.find((model) => model.model_id === selectedModelId) ?? (selectedModelId ? null : candidates[0] ?? null),
    [candidates, selectedModelId],
  );

  const trimmedKey = rawKey.trim();
  const keyReady = noKey || trimmedKey.length > 0;

  const spec: SelfTestRequestSpec | null = useMemo(() => {
    if (!selectedModel || !keyReady || modelsError || modelsLoading || authEnabled === undefined) {
      return null;
    }
    try {
      const curl = buildSelfTestCurl({
        apiFamily: selectedModel.api_family,
        openaiAcceptedFormat: selectedModel.openai_accepted_format,
        modelId: selectedModel.model_id,
        proxyKey: noKey ? "" : trimmedKey,
        openaiOperation:
          selectedModel.api_family === "openai" && selectedModel.openai_accepted_format === "dual_native"
            ? operation
            : undefined,
      });
      return { url: curl.url, method: "POST", headers: curl.headers, body: curl.body };
    } catch {
      return null;
    }
  }, [authEnabled, keyReady, modelsError, modelsLoading, noKey, operation, selectedModel, trimmedKey]);

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setRawKey("");
      setNoKeyChoice(null);
    }
    onOpenChange(nextOpen);
  };

  return (
    <>
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent size="md" data-testid="proxy-key-verify-access">
          <DialogHeader>
            <DialogTitle>{copy.verifyAccess}</DialogTitle>
            <DialogDescription>{copy.verifyAccessDescription}</DialogDescription>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-4">
            {!modelsLoading && !modelsError && selectedModelId && !selectedModel ? <OperatorCallout intent="warning" description={copy.verifyAccessModelUnavailable} /> : null}
            {authEnabled === false ? <OperatorCallout intent="info" description={copy.verifyAccessAuthOff} /> : null}
            {authEnabled === undefined ? <OperatorCallout intent="warning" description={copy.authenticationUnavailable} /> : null}
            <FieldGroup className="gap-4">
              <Field>
                <FieldLabel htmlFor="proxy-key-verify-secret">{copy.verifyAccessKeyLabel}</FieldLabel>
                <Input
                  id="proxy-key-verify-secret"
                  name="proxy-key-verify-secret"
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  className="font-mono"
                  disabled={noKey || busy}
                  placeholder={copy.verifyAccessKeyPlaceholder}
                  value={rawKey}
                  onChange={(event) => { setRawKey(event.target.value); resetResult(); }}
                />
                <FieldDescription>{copy.verifyAccessKeyHelp}</FieldDescription>
              </Field>

              <Field orientation="horizontal">
                <Checkbox
                  id="proxy-key-verify-nokey"
                  checked={noKey}
                  disabled={busy || authEnabled !== false}
                  onCheckedChange={(checked) => { setNoKeyChoice(checked === true); resetResult(); }}
                />
                <FieldLabel htmlFor="proxy-key-verify-nokey" className="font-normal">
                  {copy.verifyAccessNoKey}
                </FieldLabel>
              </Field>
              <FieldDescription>{authEnabled === true ? copy.verifyAccessAuthOn : copy.verifyAccessNoKeyHelp}</FieldDescription>

              {modelsLoading ? (
                <p className="text-sm text-muted-foreground" role="status">{copy.accessModelsLoading}</p>
              ) : modelsError ? (
                <OperatorErrorState
                  title={copy.accessModelsError}
                  action={<OperatorRetryButton onClick={onRetryModels}>{copy.retry}</OperatorRetryButton>}
                />
              ) : candidates.length === 0 ? (
                <OperatorCallout intent="info" description={copy.verifyAccessNoModel} action={<Button asChild variant="outline"><Link to="/route/models">{copy.configureModels}</Link></Button>} />
              ) : (
                <Field>
                  <FieldLabel htmlFor="proxy-key-verify-model">{copy.accessModel}</FieldLabel>
                  <Select
                    value={selectedModel?.model_id ?? ""}
                    disabled={busy}
                    onValueChange={(value) => { setSelectedModelId(value); resetResult(); }}
                  >
                    <SelectTrigger id="proxy-key-verify-model" aria-label={copy.accessModel}>
                      <SelectValue placeholder={copy.verifyAccessSelectModel} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {candidates.map((model) => (
                          <SelectItem key={model.model_id} value={model.model_id}>
                            <span className="flex min-w-0 flex-col">
                              <span className="truncate font-mono text-xs">{model.model_id}</span>
                              {model.display_name ? (
                                <span className="truncate text-xs text-muted-foreground">
                                  {model.display_name}
                                </span>
                              ) : null}
                            </span>
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              )}

              {selectedModel &&
              selectedModel.api_family === "openai" &&
              selectedModel.openai_accepted_format === "dual_native" ? (
                <Field>
                  <FieldLabel htmlFor="proxy-key-verify-operation">{copy.accessOperation}</FieldLabel>
                  <Select
                    value={operation}
                    disabled={busy}
                    onValueChange={(value) => { setOperation(value as "responses" | "chat_completions"); resetResult(); }}
                  >
                    <SelectTrigger id="proxy-key-verify-operation" aria-label={copy.accessOperation}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value="responses">{copy.accessOperationResponses}</SelectItem>
                        <SelectItem value="chat_completions">{copy.accessOperationChatCompletions}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              ) : null}
            </FieldGroup>

            {!keyReady ? (
              <p className="text-xs text-muted-foreground">{copy.verifyAccessMissingKey}</p>
            ) : null}

            <RuntimeSelfTestPanel
              key={inputRevision}
              spec={spec}
              context={{ source: "proxy_key_verify", requestedModelId: selectedModel?.model_id ?? "", proxyKey: noKey ? null : trimmedKey, explicitNoKey: noKey }}
              onBusyChange={setBusy}
              onClose={() => handleOpenChange(false)}
            />

          </DialogBody>

        </DialogContent>
      </Dialog>

    </>
  );
}
