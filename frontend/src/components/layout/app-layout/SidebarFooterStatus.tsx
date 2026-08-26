import { Link } from "@tanstack/react-router";
import { Check, Copy } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { VERSION_LABEL } from "@/lib/appVersion";
import { useLocale } from "@/i18n/useLocale";
import { copyTextToClipboard } from "@/lib/clipboard";
import { OperatorStatusBadge } from "@/shared/design-system";

/**
 * Two rows, not four. Row one is the authentication state plus the way to
 * change it; row two is the copyable build identifier.
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
  );
}
