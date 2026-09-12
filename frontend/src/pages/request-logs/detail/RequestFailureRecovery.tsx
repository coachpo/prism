import { AlertTriangle } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { OperatorCallout } from "@/shared/design-system";
import { useLocale } from "@/i18n/useLocale";
import type { RequestLogDetail } from "@/lib/types/request-logs";
import { auditScopedStatusCode } from "../auditLogView";
import { useRequestRecoveryModel } from "../useRequestRecoveryModel";
import { describeRequestFailure } from "../requestFailurePresentation";

export function RequestFailureRecovery({ request }: { request: RequestLogDetail }) {
  const { messages } = useLocale();
  const copy = messages.requestLogs;
  const recovery = describeRequestFailure({
    statusCode: auditScopedStatusCode(request.summary),
    streamOutcome: request.summary.stream_outcome,
    streamErrorKind: request.summary.stream_error_kind,
    errorPresent: Boolean(request.failure?.category),
  });
  const modelName = request.summary.attempt_target_model_id ?? request.terminal_target?.owner_model_config_id ?? request.summary.ingress_model_id;
  const modelConfigId = useRequestRecoveryModel(modelName, Boolean(recovery));
  if (!recovery) return null;
  const serviceId = request.routing.endpoint_id;
  return (
    <OperatorCallout intent="danger" icon={<AlertTriangle />}>
      <div className="flex flex-col gap-2" data-testid="request-failure-recovery">
        <h3 className="font-medium">{recovery.title}</h3>
        <p>{recovery.description}</p>
        <p>{recovery.nextStep}</p>
        <p className="text-xs">{copy.recoveryImpact}</p>
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" asChild>
            {modelConfigId !== null ? (
              <Link to="/route/models/$modelId" params={{ modelId: String(modelConfigId) }} search={{ focus_connection_id: request.terminal_target?.configured ? request.terminal_target.terminal_target_id : undefined }}>{copy.checkModel}</Link>
            ) : <Link to="/route/models" search={{ search: modelName, view: "all" }}>{copy.checkModel}</Link>}
          </Button>
          {serviceId != null ? <Button variant="outline" size="sm" asChild><a href={`/route/endpoints?endpoint_id=${serviceId}`}>{copy.checkService}</a></Button> : null}
        </div>
      </div>
    </OperatorCallout>
  );
}
