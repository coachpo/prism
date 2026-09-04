import { useEffect, useRef, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import { copyTextToClipboard } from "@/lib/clipboard";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import type { ExportRenderResponse } from "@/lib/types";

const EXPORT_FILE_NAME = "prism-pi-models.json";
const EXPORT_MIME_TYPE = "application/json;charset=utf-8";

/**
 * The generated-file sheet. Copy, Blob download, and the raw view all reuse
 * exactly one content string and its fixed file name/MIME; closing the sheet
 * or leaving the page clears the (possibly key-bearing) content from memory.
 */
export function ExportResultSheet(props: {
  result: ExportRenderResponse | null;
  onClose: () => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const [copiedSha, setCopiedSha] = useState<string | null>(null);
  const [copiedPiFragmentSha, setCopiedPiFragmentSha] = useState<string | null>(
    null,
  );
  const blobURLs = useRef<Set<string>>(new Set());
  const sha = props.result?.content_sha256 ?? null;
  const copied = sha !== null && copiedSha === sha;
  const copiedPiFragment = sha !== null && copiedPiFragmentSha === sha;
  const piProvidersFragment = props.result
    ? derivePiProvidersFragment(props.result.content)
    : null;

  const revokeBlobURLs = () => {
    for (const url of blobURLs.current) URL.revokeObjectURL(url);
    blobURLs.current.clear();
  };

  useEffect(
    () => () => {
      for (const url of blobURLs.current) URL.revokeObjectURL(url);
      blobURLs.current.clear();
    },
    [sha],
  );

  if (!props.result || !sha) return null;

  const fileName = EXPORT_FILE_NAME;

  const createContentURL = () => {
    const blob = new Blob([props.result!.content], { type: EXPORT_MIME_TYPE });
    const url = URL.createObjectURL(blob);
    blobURLs.current.add(url);
    return url;
  };

  const handleCopy = async () => {
    if (await copyTextToClipboard(props.result!.content)) {
      setCopiedSha(sha);
    }
  };

  const handleCopyPiProviderFragment = async () => {
    if (!piProvidersFragment) return;
    if (await copyTextToClipboard(piProvidersFragment)) {
      setCopiedPiFragmentSha(sha);
    }
  };

  const handleDownload = () => {
    const url = createContentURL();
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = fileName;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
    blobURLs.current.delete(url);
  };

  const handleRawView = () => {
    const url = createContentURL();
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.target = "_blank";
    anchor.rel = "noopener noreferrer";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    // Keep the URL alive while the new tab consumes it. The effect cleanup and
    // explicit close path revoke it on result replacement, route unmount, or
    // Sheet close.
  };

  const handleClose = () => {
    revokeBlobURLs();
    props.onClose();
  };

  return (
    <Sheet open onOpenChange={(open) => !open && handleClose()}>
      <SheetContent
        className="flex w-full max-w-3xl flex-col gap-4 overflow-y-auto sm:max-w-3xl"
        data-testid="export-result-sheet"
      >
        <SheetHeader>
          <SheetTitle>{copy.resultTitle}</SheetTitle>
          <SheetDescription>
            {copy.resultFileName}: <code className="font-mono">{fileName}</code>
            {" · "}
            <code className="font-mono text-xs">{EXPORT_MIME_TYPE}</code>
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-wrap items-center gap-3 text-xs">
          <Badge
            variant={props.result.warnings?.length ? "outline" : "secondary"}
          >
            {props.result.warnings?.length
              ? `${copy.warningCount.replace("{count}", String(props.result.warnings.length))}`
              : copy.noWarnings}
          </Badge>
          <span>
            SHA-256:{" "}
            <code className="font-mono">{props.result.content_sha256}</code>
          </span>
        </div>

        <pre
          data-testid="export-content-preview"
          className="max-h-[50vh] overflow-auto rounded border bg-inset p-3 font-mono text-xs whitespace-pre"
        >
          {props.result.content}
        </pre>

        {props.result.warnings && props.result.warnings.length > 0 && (
          <ul className="list-disc pl-5 text-xs text-muted-foreground">
            {props.result.warnings.map((warning) => (
              <li key={warning}>
                {(copy as Record<string, string>)[warningKey(warning)] ??
                  copy.warnGeneric}
              </li>
            ))}
          </ul>
        )}

        <p className="text-xs text-muted-foreground">
          {copy.costZeroDisclaimer}
        </p>

        {piProvidersFragment && (
          <p className="text-xs text-muted-foreground">
            {copy.piProviderMergeHint}
          </p>
        )}

        <SheetFooter className="gap-2">
          <Button onClick={() => void handleCopy()} disabled={copied}>
            {copied ? copy.copied : copy.copyButton}
          </Button>
          {piProvidersFragment && (
            <Button
              variant="outline"
              onClick={() => void handleCopyPiProviderFragment()}
              disabled={copiedPiFragment}
            >
              {copiedPiFragment ? copy.copied : copy.copyPiProviderFragment}
            </Button>
          )}
          <Button variant="outline" onClick={handleDownload}>
            {copy.downloadButton}
          </Button>
          <Button variant="outline" onClick={handleRawView}>
            {copy.rawViewButton}
          </Button>
          <Button variant="outline" onClick={handleClose}>
            {copy.closeSheet}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

/**
 * Derives the one-provider map that can be merged beneath an existing Pi
 * models.json `providers` object. The full backend content remains untouched
 * and continues to back preview, regular copy, download, and raw view.
 */
function derivePiProvidersFragment(content: string): string | null {
  try {
    const document = JSON.parse(content) as unknown;
    if (!document || typeof document !== "object" || Array.isArray(document)) {
      return null;
    }
    const providers = (document as Record<string, unknown>).providers;
    if (
      !providers ||
      typeof providers !== "object" ||
      Array.isArray(providers)
    ) {
      return null;
    }
    if (Object.keys(providers).length !== 1) return null;
    return `${JSON.stringify(providers, null, 2)}\n`;
  } catch {
    return null;
  }
}

function warningKey(code: string): string {
  const map: Record<string, string> = {
    price_no_template: "warnNoTemplate",
    price_currency_not_usd: "warnNotUsd",
    price_unit_not_per_1m: "warnNotPerMillion",
    price_incomplete_components: "warnIncomplete",
    pricing_component_missing: "warnIncomplete",
    price_reasoning_mismatch: "warnReasoningMismatch",
    price_target_conflict: "warnTargetConflict",
    price_peak_valley_unrepresentable: "warnPeakValley",
    price_tier_unrepresentable: "warnTierUnrepresentable",
    metadata_incomplete: "warnMetadataIncomplete",
    pi_source_fields_dropped: "warnPiSourceFieldsDropped",
    unsupported_input_modality: "warnUnsupportedInputModality",
    mixed_base_urls: "warnMixedBaseUrls",
  };
  return map[code] ?? code;
}
