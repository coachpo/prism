import { Link } from "@tanstack/react-router";
import { Check, Copy } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { VERSION_LABEL } from "@/lib/appVersion";
import { useLocale } from "@/i18n/useLocale";
import { copyTextToClipboard } from "@/lib/clipboard";
import { OperatorStatusBadge } from "@/shared/design-system";

/**
 * Two rows, not four. Row one is the authentication state plus the way to
 * change it; row two is the copyable build identifier.
 *
 * 收成图标轨时不整块隐藏：鉴权关闭是这套外壳里唯一常驻的 degraded 标记，
 * 768–1279 默认就是轨态，把它藏掉等于把唯一的异常提示藏掉。
 */
export function SidebarFooterStatus({ authEnabled }: { authEnabled: boolean }) {
  const { messages } = useLocale();
  const copy = messages.shell;
  const [copied, setCopied] = useState(false);

  const authLabel = authEnabled ? copy.authenticationEnabled : copy.authenticationDisabled;

  async function copyVersion() {
    if (await copyTextToClipboard(VERSION_LABEL)) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    }
  }

  return (
    <>
      {/* 轨态：状态点进设置，版本号收成一个复制按钮，两者都保住 28×28 命中区。 */}
      <div className="hidden flex-col items-center gap-1 group-data-[collapsible=icon]:flex">
        <Tooltip>
          <TooltipTrigger asChild>
            <Link
              to="/system/settings"
              search={{ scope: "instance", section: "authentication" }}
              aria-label={`${authLabel} · ${copy.viewAuthenticationSettings}`}
              data-testid="sidebar-auth-status-rail"
              className="inline-flex size-7 items-center justify-center rounded-md text-base leading-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
            >
              <span
                aria-hidden="true"
                className={authEnabled ? "text-healthy" : "text-degraded"}
              >
                {authEnabled ? "●" : "◐"}
              </span>
            </Link>
          </TooltipTrigger>
          <TooltipContent side="right">{authLabel}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              onClick={() => void copyVersion()}
              aria-label={`${copy.version} ${VERSION_LABEL} · ${copy.copyVersion}`}
              data-testid="sidebar-version-rail"
              className="size-7 text-muted-foreground"
            >
              {copied ? (
                <Check aria-hidden="true" className="size-3.5 text-healthy" />
              ) : (
                <Copy aria-hidden="true" className="size-3.5" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent side="right">{VERSION_LABEL}</TooltipContent>
        </Tooltip>
      </div>

      <div className="flex flex-col gap-1 group-data-[collapsible=icon]:hidden">
      <div
        data-testid="sidebar-auth-status"
        className="flex min-w-0 items-center justify-between gap-2"
      >
        <OperatorStatusBadge
          intent={authEnabled ? "healthy" : "degraded"}
          label={authLabel}
          preserveLabel
        />
        <Button asChild variant="link" size="sm" className="h-auto shrink-0 px-0 text-xs">
          <Link
            to="/system/settings"
            search={{ scope: "instance", section: "authentication" }}
          >
            {copy.viewAuthenticationSettings}
          </Link>
        </Button>
      </div>

      <button
        type="button"
        onClick={() => void copyVersion()}
        data-testid="sidebar-version"
        aria-label={copy.copyVersion}
        className="flex min-w-0 items-center gap-1.5 rounded-[4px] border border-border bg-inset px-1.5 py-0.5 text-left font-mono text-[0.6875rem] text-muted-foreground hover:text-foreground"
      >
        <span className="truncate" title={VERSION_LABEL}>
          {VERSION_LABEL}
        </span>
        {copied ? (
          <Check aria-hidden="true" className="size-3 shrink-0 text-healthy" />
        ) : (
          <Copy aria-hidden="true" className="size-3 shrink-0" />
        )}
      </button>
      </div>
    </>
  );
}
