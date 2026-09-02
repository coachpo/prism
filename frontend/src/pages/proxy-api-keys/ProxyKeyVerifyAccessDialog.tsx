import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
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
import { RuntimeSelfTestDialog } from "@/features/runtime-self-test/RuntimeSelfTestDialog";
import type { SelfTestRequestSpec } from "@/features/runtime-self-test/selfTestTypes";
import { runtimeSelfTestModelCandidates } from "@/features/runtime-self-test/modelCandidates";

interface ProxyKeyVerifyAccessDialogProps {
  models: ModelConfigListItem[];
  modelsError: boolean;
  modelsLoading: boolean;
  onOpenChange: (open: boolean) => void;
  onRetryModels: () => void;
  open: boolean;
}

/**
 * A standing entry point to the runtime self-test. Before this the check was
 * reachable only from inside the one-time secret dialog, so an operator could
 * never re-verify an already-delivered key.
 *
 * The pasted key never leaves the browser except as the runtime request's own
 * auth header; no management endpoint sees it.
 */
export function ProxyKeyVerifyAccessDialog({
  models,
  modelsError,
  modelsLoading,
  onOpenChange,
  onRetryModels,
  open,
}: ProxyKeyVerifyAccessDialogProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const [rawKey, setRawKey] = useState("");
  const [noKey, setNoKey] = useState(false);
  const [selectedModelId, setSelectedModelId] = useState("");
  const [operation, setOperation] = useState<"responses" | "chat_completions">("responses");
  const [selfTestOpen, setSelfTestOpen] = useState(false);

  const candidates = useMemo(() => runtimeSelfTestModelCandidates(models), [models]);
  const selectedModel = useMemo(
    () => candidates.find((model) => model.model_id === selectedModelId) ?? candidates[0] ?? null,
    [candidates, selectedModelId],
  );

  const trimmedKey = rawKey.trim();
  const keyReady = noKey || trimmedKey.length > 0;

  const spec: SelfTestRequestSpec | null = useMemo(() => {
    if (!selectedModel || !keyReady) {
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
  }, [keyReady, noKey, operation, selectedModel, trimmedKey]);

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      setRawKey("");
      setNoKey(false);
      setSelfTestOpen(false);
    }
    onOpenChange(nextOpen);
  };

  return (
    <>
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent className="sm:max-w-xl" data-testid="proxy-key-verify-access">
          <DialogHeader>
            <DialogTitle>{copy.verifyAccess}</DialogTitle>
            <DialogDescription>{copy.verifyAccessDescription}</DialogDescription>
          </DialogHeader>
          <DialogBody className="flex flex-col gap-4">
            <FieldGroup className="gap-4">
              <Field>
                <FieldLabel htmlFor="proxy-key-verify-secret">{copy.verifyAccessKeyLabel}</FieldLabel>
                <Input
                  id="proxy-key-verify-secret"
                  name="proxy-key-verify-secret"
                  autoComplete="off"
                  spellCheck={false}
                  className="font-mono"
                  disabled={noKey}
                  placeholder={copy.verifyAccessKeyPlaceholder}
                  value={rawKey}
                  onChange={(event) => setRawKey(event.target.value)}
                />
                <FieldDescription>{copy.verifyAccessKeyHelp}</FieldDescription>
              </Field>

              <Field orientation="horizontal">
                <Checkbox
                  id="proxy-key-verify-nokey"
                  checked={noKey}
                  onCheckedChange={(checked) => setNoKey(checked === true)}
                />
                <FieldLabel htmlFor="proxy-key-verify-nokey" className="font-normal">
                  {copy.verifyAccessNoKey}
                </FieldLabel>
              </Field>
              <FieldDescription>{copy.verifyAccessNoKeyHelp}</FieldDescription>

              {modelsLoading ? (
                <p className="text-sm text-muted-foreground">{copy.accessModelsLoading}</p>
              ) : modelsError ? (
                <OperatorErrorState
                  title={copy.accessModelsError}
                  action={<OperatorRetryButton onClick={onRetryModels}>{copy.retry}</OperatorRetryButton>}
                />
              ) : candidates.length === 0 ? (
                <p className="text-sm text-muted-foreground">{copy.verifyAccessNoModel}</p>
              ) : (
                <Field>
                  <FieldLabel htmlFor="proxy-key-verify-model">{copy.accessModel}</FieldLabel>
                  <Select
                    value={selectedModel?.model_id ?? ""}
                    onValueChange={(value) => setSelectedModelId(value)}
                  >
                    <SelectTrigger id="proxy-key-verify-model" aria-label={copy.accessModel}>
                      <SelectValue />
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
                    onValueChange={(value) => setOperation(value as "responses" | "chat_completions")}
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

            <OperatorCallout intent="warning" description={copy.selfTestCostWarning} />

            {!keyReady ? (
              <p className="text-xs text-muted-foreground">{copy.verifyAccessMissingKey}</p>
            ) : null}
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => handleOpenChange(false)}>
              {messages.common.close}
            </Button>
            <Button type="button" disabled={!spec} onClick={() => setSelfTestOpen(true)}>
              {copy.selfTestRun}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <RuntimeSelfTestDialog
        open={selfTestOpen}
        onOpenChange={setSelfTestOpen}
        spec={spec}
        context={{
          source: "proxy_key_verify",
          requestedModelId: selectedModel?.model_id ?? "",
          proxyKey: noKey ? null : trimmedKey,
          explicitNoKey: noKey,
        }}
      />
    </>
  );
}
