/**
 * Shared runtime self-test dialog (Proxy Key SPEC §9).
 *
 * One surface reused by the generated-secret dialog, model detail and
 * endpoint detail. It sends one direct request through the allowlisted
 * runtime ingress and renders the four-layer result; telemetry is reconciled
 * by exact ingress ID with a bounded poll and an explicit recheck action.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { ArrowRight, Loader2, RefreshCw, Send, X } from "lucide-react";
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
import { useLocale } from "@/i18n/useLocale";
import { OperatorInsetPanel } from "@/shared/design-system";
import {
  buildSelfTestResult,
  reconcileSelfTestTelemetry,
  runRuntimeSelfTestDirect,
  SelfTestAbortedError,
} from "./selfTestRunner";
import {
  SELF_TEST_REQUESTS_HANDOFF_PATH,
  type RuntimeSelfTestResult,
  type SelfTestEntryContext,
  type SelfTestRequestSpec,
} from "./selfTestTypes";
import { Link } from "@tanstack/react-router";

interface RuntimeSelfTestDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  spec: SelfTestRequestSpec | null;
  context: SelfTestEntryContext;
}

type RunState = "idle" | "running" | "polling" | "done";

export function RuntimeSelfTestDialog({ open, onOpenChange, spec, context }: RuntimeSelfTestDialogProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const [runState, setRunState] = useState<RunState>("idle");
  const [result, setResult] = useState<RuntimeSelfTestResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Reset-on-close uses a keyed remount at the call site; this component
  // never mutates state from an effect.
  useEffect(() => {
    if (!open) {
      abortRef.current?.abort();
      abortRef.current = null;
    }
  }, [open]);

  const runSelfTest = useCallback(async () => {
    if (!spec) {
      return;
    }
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setRunState("running");
    setResult(null);
    setError(null);

    try {
      const direct = await runRuntimeSelfTestDirect(spec, context, controller.signal);
      if (controller.signal.aborted) {
        return;
      }
      setRunState("polling");
      let telemetry: Awaited<ReturnType<typeof reconcileSelfTestTelemetry>> | null = null;
      if (direct.ingressRequestId) {
        try {
          telemetry = await reconcileSelfTestTelemetry(direct.ingressRequestId, controller.signal);
        } catch (pollError) {
          if (pollError instanceof SelfTestAbortedError) {
            return;
          }
          telemetry = null;
        }
      }
      if (controller.signal.aborted) {
        return;
      }
      setResult(buildSelfTestResult(direct, context, telemetry));
      setRunState("done");
    } catch (runError) {
      if (runError instanceof SelfTestAbortedError || (runError instanceof DOMException && runError.name === "AbortError")) {
        return;
      }
      setResult(
        buildSelfTestResult(
          { ingressRequestId: null, statusCode: null, safeSummary: null },
          context,
          null,
        ),
      );
      setError(runError instanceof Error ? runError.message : messages.proxyApiKeysData.selfTestFailed);
      setRunState("done");
    }
  }, [context, messages.proxyApiKeysData.selfTestFailed, spec]);

  const recheckTelemetry = useCallback(async () => {
    if (!result?.ingressRequestId) {
      return;
    }
    const controller = new AbortController();
    abortRef.current = controller;
    setRunState("polling");
    try {
      const telemetry = await reconcileSelfTestTelemetry(result.ingressRequestId, controller.signal);
      if (!controller.signal.aborted) {
        setResult(
          buildSelfTestResult(
            { ingressRequestId: result.ingressRequestId, statusCode: result.direct.statusCode, safeSummary: result.direct.safeSummary },
            context,
            telemetry,
          ),
        );
        setRunState("done");
      }
    } catch {
      if (!controller.signal.aborted) {
        setRunState("done");
      }
    }
  }, [context, result]);

  const busy = runState === "running" || runState === "polling";
  const ingressId = result?.ingressRequestId ?? null;
  const requestsHandoffUrl = ingressId
    ? `${SELF_TEST_REQUESTS_HANDOFF_PATH}${encodeURIComponent(ingressId)}`
    : null;

  return (
    <Dialog open={open} onOpenChange={(next) => (next ? undefined : onOpenChange(false))}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{copy.selfTestTitle}</DialogTitle>
          <DialogDescription>{copy.selfTestDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          {spec ? (
            <OperatorInsetPanel className="flex-col gap-1 text-xs text-muted-foreground">
              <p className="break-all font-mono">{spec.method} {spec.url}</p>
              <p className="break-all font-mono text-[11px]">
                {Object.entries(spec.headers)
                  .map(([name]) => name)
                  .join(" · ")}
              </p>
            </OperatorInsetPanel>
          ) : null}

          {error ? <p className="text-sm text-destructive">{error}</p> : null}

          {busy ? (
            <div className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
              <Loader2 className="h-4 w-4 animate-spin" />
              {runState === "running" ? copy.selfTestRunning : copy.selfTestReconciling}
            </div>
          ) : null}

          {result ? <SelfTestResultPanel result={result} /> : null}

          {result?.telemetryState === "timed_out" && result.ingressRequestId ? (
            <p className="text-sm text-muted-foreground">
              {copy.selfTestTelemetryPending} <code className="break-all font-mono text-xs">{result.ingressRequestId}</code>
            </p>
          ) : null}

          {requestsHandoffUrl ? (
            <p className="text-sm">
              <Link to={requestsHandoffUrl} className="inline-flex items-center gap-1 font-medium text-primary underline-offset-4 hover:underline">
                {copy.selfTestViewInRequests} <ArrowRight className="h-3.5 w-3.5" />
              </Link>
            </p>
          ) : null}
        </DialogBody>
        <DialogFooter>
          {result && result.ingressRequestId && result.telemetryState === "timed_out" ? (
            <Button type="button" variant="outline" onClick={() => void recheckTelemetry()} disabled={busy}>
              <RefreshCw className="h-4 w-4" />
              {copy.selfTestRecheck}
            </Button>
          ) : null}
          {busy ? (
            <Button type="button" variant="outline" onClick={() => abortRef.current?.abort()}>
              <X className="h-4 w-4" />
              {copy.selfTestCancel}
            </Button>
          ) : (
            <Button type="button" onClick={() => void runSelfTest()} disabled={!spec}>
              <Send className="h-4 w-4" />
              {result ? copy.selfTestRunAgain : copy.selfTestRun}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function SelfTestResultPanel({ result }: { result: RuntimeSelfTestResult }) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const rows: Array<{ label: string; tone: "ok" | "warn" | "error" | "muted"; value: string }> = [];

  const credentialText =
    result.credential.attributionState === "identified"
      ? copy.selfTestCredentialIdentified(result.credential.observedProxyApiKeyId ?? "—")
      : result.credential.attributionState === "none"
        ? copy.selfTestCredentialNone
        : result.credential.attributionState === "unknown"
          ? copy.selfTestCredentialUnknown
          : copy.selfTestCredentialPending;
  rows.push({
    label: copy.selfTestLayerCredential,
    tone: result.credential.attributionState === "identified" ? "ok" : result.credential.attributionState === "none" ? "warn" : "muted",
    value: credentialText,
  });

  const routingText =
    result.routing.state === "resolved"
      ? copy.selfTestRoutingResolved(result.routing.resolvedModelId ?? result.routing.requestedModelId)
      : result.routing.state === "not_reached"
        ? copy.selfTestRoutingNotReached
        : copy.selfTestEvidencePending;
  rows.push({ label: copy.selfTestLayerRouting, tone: result.routing.state === "resolved" ? "ok" : "muted", value: routingText });

  const executionText =
    result.execution.state === "completed"
      ? copy.selfTestExecutionCompleted(result.execution.endpointLabelSnapshot ?? copy.selfTestExecutionUnknownTarget)
      : result.execution.state === "failed"
        ? copy.selfTestExecutionFailed
        : result.execution.state === "not_reached"
          ? copy.selfTestExecutionNotReached
          : copy.selfTestEvidencePending;
  rows.push({
    label: copy.selfTestLayerExecution,
    tone: result.execution.state === "completed" ? "ok" : result.execution.state === "failed" ? "error" : "muted",
    value: executionText,
  });

  const pricingText =
    result.pricing.state === "priced"
      ? copy.selfTestPricingPriced(result.pricing.costMicros ?? "0", result.pricing.currency ?? "")
      : result.pricing.state === "unpriced"
        ? copy.selfTestPricingUnpriced
        : result.pricing.state === "ineligible"
          ? copy.selfTestPricingIneligible
          : copy.selfTestEvidencePending;
  rows.push({ label: copy.selfTestLayerPricing, tone: result.pricing.state === "priced" ? "ok" : result.pricing.state === "unpriced" ? "warn" : "muted", value: pricingText });

  const directTone =
    result.direct.state === "succeeded"
      ? copy.selfTestDirectSucceeded(result.direct.statusCode ?? "—")
      : result.direct.state === "http_error"
        ? copy.selfTestDirectHttpError(result.direct.statusCode ?? "—", result.direct.safeSummary ?? "")
        : result.direct.state === "network_error"
          ? copy.selfTestDirectNetworkError
          : copy.selfTestDirectCancelled;

  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm">
        <span className={result.direct.state === "succeeded" ? "font-medium text-healthy" : "font-medium text-destructive"}>
          {directTone}
        </span>
        {result.direct.safeSummary ? <span className="ml-2 text-muted-foreground">{result.direct.safeSummary}</span> : null}
      </p>
      <div className="flex flex-col gap-2">
        {rows.map((row) => (
          <div key={row.label} className="flex items-start justify-between gap-3 text-sm">
            <span className="shrink-0 text-muted-foreground">{row.label}</span>
            <span
              className={
                row.tone === "ok"
                  ? "text-right text-foreground"
                  : row.tone === "error"
                    ? "text-right text-destructive"
                    : row.tone === "warn"
                      ? "text-right text-degraded"
                      : "text-right text-muted-foreground"
              }
            >
              {row.value}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
