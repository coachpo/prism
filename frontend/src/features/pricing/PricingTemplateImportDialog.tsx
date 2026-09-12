import { useRef, useState } from "react";
import { FileUp, Loader2, Upload } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import type {
  PricingTemplateCreate,
  PricingTemplateImportMode,
  PricingTemplateImportRequest,
} from "@/lib/types";
import { OperatorCallout, OperatorInsetPanel } from "@/shared/design-system";

interface PricingTemplateImportDialogProps {
  importing: boolean;
  serverError?: string | null;
  onClose: () => void;
  onImport: (request: PricingTemplateImportRequest) => Promise<boolean>;
  onOpenChange: (open: boolean) => void;
  open: boolean;
}

function parseImportJson(
  raw: string,
  mode: PricingTemplateImportMode,
): PricingTemplateImportRequest {
  const parsed = JSON.parse(raw) as unknown;
  if (Array.isArray(parsed)) {
    return {
	      schema_version: 3,
      mode,
      templates: parsed as PricingTemplateCreate[],
    };
  }
  if (
    parsed &&
    typeof parsed === "object" &&
    Array.isArray((parsed as { templates?: unknown }).templates)
  ) {
    return {
      ...(parsed as Partial<PricingTemplateImportRequest>),
	      schema_version: 3,
      mode,
      templates: (parsed as { templates: PricingTemplateCreate[] }).templates,
    };
  }
  throw new Error("templates");
}

export function PricingTemplateImportDialog({
  importing,
  serverError,
  onClose,
  onImport,
  onOpenChange,
  open,
}: PricingTemplateImportDialogProps) {
  const { messages } = useLocale();
  const copy = messages.pricing;
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const [mode, setMode] = useState<PricingTemplateImportMode>("upsert_by_name");
  const [rawJson, setRawJson] = useState("");
  const [fileName, setFileName] = useState("");
  const [readingFile, setReadingFile] = useState(false);
  const fileReadGeneration = useRef(0);
  const [error, setError] = useState<string | null>(null);

  const resetDraft = () => {
    setMode("upsert_by_name");
    setRawJson("");
    setFileName("");
    setReadingFile(false);
    fileReadGeneration.current += 1;
    setError(null);
  };

  const closeDialog = () => {
    if (importing) return;
    resetDraft();
    onClose();
  };

  const selectFile = async (file: File) => {
    const generation = ++fileReadGeneration.current;
    setError(null);
    setRawJson("");
    setFileName(file.name);
    setReadingFile(true);
    try {
      const contents = await file.text();
      if (generation === fileReadGeneration.current) setRawJson(contents);
    } catch {
      if (generation === fileReadGeneration.current) setError(copy.fileReadFailed);
    } finally {
      if (generation === fileReadGeneration.current) setReadingFile(false);
    }
  };

  const handleSubmit = async () => {
    let request: PricingTemplateImportRequest;
    try {
      setError(null);
      request = parseImportJson(rawJson, mode);
    } catch {
      setError(copy.importInvalidJson);
      return;
    }
    if (await onImport(request)) {
      resetDraft();
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) closeDialog();
        else onOpenChange(nextOpen);
      }}
    >
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>{copy.importTitle}</DialogTitle>
          <DialogDescription>{copy.importDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex min-h-0 flex-col gap-4 overflow-y-auto pr-1">
          {error ? <OperatorCallout intent="danger" description={error} /> : null}
          {serverError ? <OperatorCallout intent="danger" title={copy.importFailed} description={serverError} /> : null}
          <OperatorInsetPanel>
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
              <div className="grid gap-2">
                <Label htmlFor="pricing-import-mode">
                  {copy.importModeLabel}
                </Label>
                <Select
                  value={mode}
                  disabled={importing}
                  onValueChange={(value) =>
                    setMode(value as PricingTemplateImportMode)
                  }
                >
                  <SelectTrigger id="pricing-import-mode">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="upsert_by_name">
                        {copy.importModeUpsert}
                      </SelectItem>
                      <SelectItem value="create_only">
                        {copy.importModeCreateOnly}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
              <Button
                type="button"
                variant="outline"
                disabled={importing || readingFile}
                onClick={() => fileInputRef.current?.click()}
              >
                <Upload data-icon="inline-start" />
                {fileName ? copy.changeFile : copy.chooseFile}
              </Button>
              <input
                ref={fileInputRef}
                className="hidden"
                type="file"
                aria-label={copy.chooseFile}
                accept="application/json,.json"
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  if (!file) return;
                  void selectFile(file);
                  event.currentTarget.value = "";
                }}
              />
            </div>
          </OperatorInsetPanel>
          <p className="text-sm text-muted-foreground" role="status">{fileName ? copy.selectedFile(fileName) : copy.noFileSelected}</p>
        </DialogBody>
        <DialogFooter className="sm:justify-between">
          <Button type="button" variant="outline" disabled={importing} onClick={closeDialog}>
            {messages.pricingTemplatesUi.close}
          </Button>
          <Button
            type="button"
            disabled={importing || readingFile || rawJson.trim().length === 0}
            onClick={() => void handleSubmit()}
          >
            {importing ? (
              <Loader2 data-icon="inline-start" className="animate-spin" />
            ) : (
              <FileUp data-icon="inline-start" />
            )}
            {copy.importPreview}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
