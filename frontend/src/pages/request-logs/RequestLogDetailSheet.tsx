import { Link } from "@tanstack/react-router";
import { Terminal } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import type { RequestLogDetail } from "@/lib/types";
import { RequestLogOverviewTab } from "./detail/RequestLogOverviewTab";

interface RequestLogDetailSheetProps {
  request: RequestLogDetail | null;
  open: boolean;
  onClose: () => void;
  formatTimestamp: (iso: string) => string;
}

export function RequestLogDetailSheet({
  request,
  open,
  onClose,
  formatTimestamp,
}: RequestLogDetailSheetProps) {
  const { messages } = useLocale();
  const hasRequestContext = Boolean(request);

  return (
    <Sheet open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onClose(); }}>
      <SheetContent
        className="w-full overflow-x-hidden overflow-y-auto border-l border-outline-variant bg-surface px-0 sm:max-w-3xl xl:max-w-[72rem]"
        data-clipboard-fallback-root=""
        data-testid="request-log-detail-sheet"
      >
        <div className="flex min-h-full flex-col gap-4 px-4 pb-5 pt-4 sm:px-6">
          <SheetHeader className="gap-2 pr-8 text-left">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Terminal className="h-3.5 w-3.5" />
              <span>{messages.requestLogs.technicalInspection}</span>
            </div>
            <SheetTitle className="text-xl font-semibold tracking-tight">
              {messages.requestLogs.requestTitle(request?.summary.id ?? "")}
            </SheetTitle>
            <SheetDescription className="text-sm text-muted-foreground">
              {messages.requestLogs.detailDescription}
              {hasRequestContext ? ` ${messages.requestLogs.requestedModel} / ${messages.requestLogs.finalTargetModel}.` : ""}
            </SheetDescription>
          </SheetHeader>

          {request && (
            <div className="flex min-w-0 flex-col gap-3">
              <div className="flex justify-end">
                <Button variant="outline" size="sm" asChild>
                  <Link
                    to="/observe/requests/$requestId/audit"
                    params={{ requestId: String(request.summary.id) }}
                  >
                    <Terminal data-icon="inline-start" />
                    {messages.requestLogs.openDedicatedAuditPage}
                  </Link>
                </Button>
              </div>
              <RequestLogOverviewTab
                request={request}
                formatTimestamp={formatTimestamp}
              />
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}
