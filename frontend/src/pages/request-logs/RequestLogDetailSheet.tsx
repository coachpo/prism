import { FileText, Terminal } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { RequestLogDetail } from "@/lib/types";
import type { DetailTab } from "./queryParams";
import { useAuditDetail } from "./useAuditDetail";
import { RequestLogAuditTab } from "./detail/RequestLogAuditTab";
import { RequestLogOverviewTab } from "./detail/RequestLogOverviewTab";
import type { RequestLogModelResolver } from "./columns";

interface RequestLogDetailSheetProps {
  request: RequestLogDetail | null;
  open: boolean;
  activeTab: DetailTab;
  onTabChange: (tab: DetailTab) => void;
  onClose: () => void;
  formatTimestamp: (iso: string) => string;
  resolveModelLabel: RequestLogModelResolver;
}

export function RequestLogDetailSheet({
  request,
  open,
  activeTab,
  onTabChange,
  onClose,
  formatTimestamp,
  resolveModelLabel,
}: RequestLogDetailSheetProps) {
  const { messages } = useLocale();
  const { audits, loading: auditLoading, error: auditError } = useAuditDetail({
    requestLogId: request?.summary.id ?? null,
    enabled: open && activeTab === "audit",
  });
  const hasResolvedTargetContext = Boolean(
    request?.summary.resolved_target_model_id
      && request.summary.resolved_target_model_id !== request.summary.model_id,
  );

  return (
    <Sheet open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onClose(); }}>
      <SheetContent
        className="w-full overflow-y-auto border-l border-border/70 bg-background/98 px-0 sm:max-w-3xl xl:max-w-[72rem]"
        data-testid="request-log-detail-sheet"
      >
        <div className="space-y-4 px-5 pb-5 pt-4 sm:px-6">
          <SheetHeader className="space-y-2 pr-8 text-left">
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Terminal className="h-3.5 w-3.5" />
              <span>{messages.requestLogs.technicalInspection}</span>
            </div>
            <SheetTitle className="text-xl font-semibold tracking-tight">
              {messages.requestLogs.requestTitle(request?.summary.id ?? "")}
            </SheetTitle>
            <SheetDescription className="text-sm text-muted-foreground">
              {messages.requestLogs.detailDescription}
              {hasResolvedTargetContext ? ` ${messages.requestLogs.requestedModel} / ${messages.requestLogs.resolvedTarget}.` : ""}
            </SheetDescription>
          </SheetHeader>

          {request && (
            <Tabs value={activeTab} onValueChange={(value) => onTabChange(value as DetailTab)} className="space-y-3">
              <TabsList className="grid h-10 w-full grid-cols-2 rounded-lg bg-muted/70 p-0.5">
                <TabsTrigger value="overview" className="gap-2 rounded-md text-sm font-medium">
                  <FileText className="h-4 w-4" />
                  {messages.requestLogs.overview}
                </TabsTrigger>
                <TabsTrigger value="audit" className="gap-2 rounded-md text-sm font-medium">
                  <Terminal className="h-4 w-4" />
                  {messages.requestLogs.audit}
                </TabsTrigger>
              </TabsList>

              <TabsContent value="overview" className="mt-0">
                <RequestLogOverviewTab
                  request={request}
                  formatTimestamp={formatTimestamp}
                  resolveModelLabel={resolveModelLabel}
                />
              </TabsContent>

              <TabsContent value="audit" className="mt-0">
                <RequestLogAuditTab
                  audits={audits}
                  loading={auditLoading}
                  error={auditError}
                  formatTimestamp={formatTimestamp}
                />
              </TabsContent>
            </Tabs>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}
