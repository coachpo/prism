import { useRef, useState } from "react"
import { FileUp, Loader2, Upload } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/i18n/useLocale"
import type { PricingTemplateCreate, PricingTemplateImportMode, PricingTemplateImportRequest } from "@/lib/types"
import { OperatorCallout, OperatorInsetPanel } from "@/shared/design-system"

interface PricingTemplateImportDialogProps {
  importing: boolean
  onClose: () => void
  onImport: (request: PricingTemplateImportRequest) => Promise<boolean>
  onOpenChange: (open: boolean) => void
  open: boolean
}

function parseImportJson(raw: string, mode: PricingTemplateImportMode): PricingTemplateImportRequest {
  const parsed = JSON.parse(raw) as unknown
  if (Array.isArray(parsed)) {
    return { mode, templates: parsed as PricingTemplateCreate[] }
  }
  if (parsed && typeof parsed === "object" && Array.isArray((parsed as { templates?: unknown }).templates)) {
    return { ...(parsed as PricingTemplateImportRequest), mode }
  }
  throw new Error("templates")
}

export function PricingTemplateImportDialog({
  importing,
  onClose,
  onImport,
  onOpenChange,
  open,
}: PricingTemplateImportDialogProps) {
  const { messages } = useLocale()
  const copy = messages.pricing
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const [mode, setMode] = useState<PricingTemplateImportMode>("upsert_by_name")
  const [rawJson, setRawJson] = useState("")
  const [error, setError] = useState<string | null>(null)

  const resetDraft = () => {
    setMode("upsert_by_name")
    setRawJson("")
    setError(null)
  }

  const closeDialog = () => {
    resetDraft()
    onClose()
  }

  const handleSubmit = async () => {
    let request: PricingTemplateImportRequest
    try {
      setError(null)
      request = parseImportJson(rawJson, mode)
    } catch {
      setError(copy.importInvalidJson)
      return
    }
    if (await onImport(request)) {
      resetDraft()
    }
  }

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) closeDialog(); else onOpenChange(nextOpen) }}>
      <DialogContent className="max-h-[90vh] sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{copy.importTitle}</DialogTitle>
          <DialogDescription>{copy.importDescription}</DialogDescription>
        </DialogHeader>
        {error ? <OperatorCallout intent="danger" description={error} /> : null}
        <DialogBody className="flex min-h-0 flex-col gap-4 overflow-y-auto pr-1">
          <OperatorInsetPanel>
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end">
              <div className="grid gap-2">
                <Label htmlFor="pricing-import-mode">{copy.importModeUpsert}</Label>
                <Select value={mode} onValueChange={(value) => setMode(value as PricingTemplateImportMode)}>
                  <SelectTrigger id="pricing-import-mode">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="upsert_by_name">{copy.importModeUpsert}</SelectItem>
                      <SelectItem value="create_only">{copy.importModeCreateOnly}</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </div>
              <Button type="button" variant="outline" onClick={() => fileInputRef.current?.click()}>
                <Upload data-icon="inline-start" />
                {copy.importButton}
              </Button>
              <input
                ref={fileInputRef}
                className="hidden"
                type="file"
                accept="application/json,.json"
                onChange={(event) => {
                  const file = event.target.files?.[0]
                  if (!file) return
                  void file.text().then(setRawJson)
                  event.currentTarget.value = ""
                }}
              />
            </div>
          </OperatorInsetPanel>
          <div className="grid gap-2">
            <Label htmlFor="pricing-import-json">{copy.importTitle}</Label>
            <Textarea
              id="pricing-import-json"
              className="min-h-64 font-mono text-xs"
              value={rawJson}
              onChange={(event) => setRawJson(event.target.value)}
              spellCheck={false}
            />
          </div>
        </DialogBody>
        <DialogFooter className="sm:justify-between">
          <Button type="button" variant="outline" onClick={closeDialog}>{messages.pricingTemplatesUi.close}</Button>
          <Button type="button" disabled={importing || rawJson.trim().length === 0} onClick={() => void handleSubmit()}>
            {importing ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <FileUp data-icon="inline-start" />}
            {copy.importButton}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
