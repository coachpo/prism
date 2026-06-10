import { Link } from "react-router-dom";
import { FileText, Terminal } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { RequestLogDetail } from "@/lib/types";
import type { DetailTab } from "./queryParams";
import { useAuditDetail } from "./useAuditDetail";
import { RequestLogAuditTab } from "./detail/RequestLogAuditTab";
import { RequestLogOverviewTab } from "./detail/RequestLogOverviewTab";

interface RequestLogDetailSheetProps {
  request: RequestLogDetail | null;
  open: boolean;
  activeTab: DetailTab;
  onTabChange: (tab: DetailTab) => void;
  onClose: () => void;
  formatTimestamp: (iso: string) => string;
}

export function RequestLogDetailSheet({
  request,
  open,
  activeTab,
  onTabChange,
  onClose,
  formatTimestamp,
}: RequestLogDetailSheetProps) {
  const { messages } = useLocale();
  const { audits, loading: auditLoading, state: auditState } = useAuditDetail({
    requestLogId: request?.summary.id ?? null,
    requestCreatedAt: request?.summary.created_at ?? null,
    auditEnabledAtRequest: request?.routing.audit_enabled_at_request ?? false,
    auditCaptureBodiesAtRequest: request?.routing.audit_capture_bodies_at_request ?? false,
    enabled: open && activeTab === "audit",
  });
  const hasRequestContext = Boolean(request);

  return (
    <Sheet open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onClose(); }}>
      <SheetContent
        className="w-full overflow-x-hidden overflow-y-auto border-l border-border/70 bg-background/98 px-0 sm:max-w-3xl xl:max-w-[72rem]"
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
            <Tabs value={activeTab} onValueChange={(value) => onTabChange(value as DetailTab)} className="flex min-w-0 flex-col gap-3">
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

              <TabsContent value="overview" className="mt-0 min-w-0">
                <RequestLogOverviewTab
                  request={request}
                  formatTimestamp={formatTimestamp}
                />
              </TabsContent>

              <TabsContent value="audit" className="mt-0 min-w-0">
                <div className="mb-3 flex justify-end">
                  <Button variant="outline" size="sm" asChild>
                    <Link to={`/request-logs/${request.summary.id}/audit`}>
                      <Terminal data-icon="inline-start" />
                      {messages.requestLogs.openDedicatedAuditPage}
                    </Link>
                  </Button>
                </div>
                <RequestLogAuditTab
                  apiFamily={request.summary.api_family}
                  audits={audits}
                  loading={auditLoading}
                  state={auditState}
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
