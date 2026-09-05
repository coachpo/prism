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
import { cn } from "@/lib/utils";
import { OperatorCallout, OperatorTypeBadge } from "@/shared/design-system";
import type { ExportRenderResponse } from "@/lib/types";

const EXPORT_FILE_NAME = "prism-pi-models.json";
const EXPORT_MIME_TYPE = "application/json;charset=utf-8";
// 「已复制」是一次瞬时反馈，不是按钮的终态：剪贴板随时会被别的内容顶掉。
const COPY_FEEDBACK_MS = 2000;
const PREVIEW_ID = "export-content-preview";

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
  const [copyFailed, setCopyFailed] = useState(false);
  const [previewExpanded, setPreviewExpanded] = useState(false);
  const blobURLs = useRef<Set<string>>(new Set());
  const copyResetTimers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const sha = props.result?.content_sha256 ?? null;
  const copied = sha !== null && copiedSha === sha;
  const copiedPiFragment = sha !== null && copiedPiFragmentSha === sha;
  const piProvidersFragment = props.result
    ? derivePiProvidersFragment(props.result.content)
    : null;
  const warnings = props.result?.warnings ?? [];

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

  useEffect(
    () => () => {
      for (const timer of copyResetTimers.current) clearTimeout(timer);
      copyResetTimers.current = [];
    },
    [],
  );

  const scheduleCopyReset = (reset: () => void) => {
    copyResetTimers.current.push(setTimeout(reset, COPY_FEEDBACK_MS));
  };

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
      setCopyFailed(false);
      setCopiedSha(sha);
      scheduleCopyReset(() => setCopiedSha(null));
      return;
    }
    setCopiedSha(null);
    setCopyFailed(true);
  };

  const handleCopyPiProviderFragment = async () => {
    if (!piProvidersFragment) return;
    if (await copyTextToClipboard(piProvidersFragment)) {
      setCopyFailed(false);
      setCopiedPiFragmentSha(sha);
      scheduleCopyReset(() => setCopiedPiFragmentSha(null));
      return;
    }
    setCopiedPiFragmentSha(null);
    setCopyFailed(true);
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
        size="lg"
        className="flex flex-col gap-4 overflow-y-auto"
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

        {/* 抽屉开着的时候只能就地报错。复制失败一直是静默的，而这一页唯一的
            产出就是要粘出去的那段文本。 */}
        {copyFailed ? (
          <OperatorCallout
            intent="danger"
            data-testid="export-copy-failed"
            description={copy.copyFailedInline}
          />
        ) : null}

        <div className="flex flex-wrap items-center gap-3 text-xs">
          {/* 「无提示 / 3 条提示」不能只靠颜色区分：两个 Badge variant 的前景
              都落在 --text，扫一眼是同一个东西。 */}
          <OperatorTypeBadge
            intent={warnings.length > 0 ? "degraded" : "muted"}
            preserveLabel
            label={
              warnings.length > 0
                ? `⚠ ${copy.warningCount.replace("{count}", String(warnings.length))}`
                : `✓ ${copy.noWarnings}`
            }
          />
          <span>
            SHA-256:{" "}
            <code className="font-mono">{props.result.content_sha256}</code>
          </span>
        </div>

        {/* 缺失的 cost 组会让客户端把未配置价格显示成 0：这正是诚实契约要求
            突出的事实，不能是代码块下方最弱的一行灰字。 */}
        {warnings.length > 0 && (
          <OperatorCallout
            intent="warning"
            title={copy.warningsTitle}
            data-testid="export-result-warnings"
            description={
              <ul className="list-disc pl-5">
                {warnings.map((warning) => (
                  <li key={warning}>
                    {(copy as Record<string, string>)[warningKey(warning)] ??
                      copy.warnGeneric}
                  </li>
                ))}
              </ul>
            }
          />
        )}

        {/* 预览默认只给半屏，但它必须能被键盘聚焦滚动，也必须能整段展开：
            操作者即将粘出去的是这里的全文，不是可见的那 385px。 */}
        <div className="flex flex-col gap-1">
          <div className="flex justify-end">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-controls={PREVIEW_ID}
              aria-expanded={previewExpanded}
              onClick={() => setPreviewExpanded((expanded) => !expanded)}
            >
              {previewExpanded ? copy.previewCollapse : copy.previewExpand}
            </Button>
          </div>
          <pre
            id={PREVIEW_ID}
            data-testid="export-content-preview"
            role="region"
            aria-label={copy.previewRegionLabel}
            tabIndex={0}
            className={cn(
              "overflow-auto rounded border bg-inset p-3 font-mono text-xs leading-[18px] whitespace-pre",
              previewExpanded ? "max-h-none" : "max-h-[50vh]",
            )}
          >
            {props.result.content}
          </pre>
        </div>

        <p className="text-xs text-muted-foreground">
          {copy.costZeroDisclaimer}
        </p>

        {piProvidersFragment && (
          <p className="text-xs text-muted-foreground">
            {copy.piProviderMergeHint}
          </p>
        )}

        {/* 五个全宽竖排按钮占掉 202px 高，把内容挤出屏幕；宽屏一行放得下。 */}
        <SheetFooter className="gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-end">
          <Button onClick={() => void handleCopy()}>
            {copied ? copy.copied : copy.copyButton}
          </Button>
          {piProvidersFragment && (
            <Button
              variant="outline"
              onClick={() => void handleCopyPiProviderFragment()}
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
