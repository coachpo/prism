import { useCallback, useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { api } from "@/lib/api";
import type { GlobalCurrentStateCompleteness } from "@/lib/types";
import {
  OperatorClippedBadge,
  OperatorErrorState,
  OperatorRetryButton,
  OperatorSectionCard,
  OperatorStatusBadge,
} from "@/shared/design-system";

type CompletenessState = {
  phase: "loading" | "ready" | "error";
  data: GlobalCurrentStateCompleteness | null;
  error: string | null;
};

const LOADING_STATE: CompletenessState = {
  phase: "loading",
  data: null,
  error: null,
};

/**
 * Routing health is a page of its own now. The dashboard keeps a triage entry
 * here, carrying the best-effort caveat so nobody reads the event ledger as
 * complete before they open it.
 *
 * The card header answers the landing page's first question — is it healthy
 * right now — instead of promising cooldown and ban state and then rendering
 * a single link. Only the cohort-wide completeness block is read (`limit: 1`),
 * never the rows: this card ranks the deployment, it does not list it.
 */
export function RoutingHealthEntryCard() {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;
  const [reloadToken, setReloadToken] = useState(0);
  const [snapshot, setSnapshot] = useState<{
    key: number;
    state: CompletenessState;
  }>(() => ({ key: 0, state: LOADING_STATE }));
  // The visible state is bound to the read it belongs to, so a retry shows the
  // pending state without a synchronous setState inside the effect.
  const state = snapshot.key === reloadToken ? snapshot.state : LOADING_STATE;
  const load = useCallback(() => setReloadToken((token) => token + 1), []);

  useEffect(() => {
    let cancelled = false;
    void api.loadbalance
      .listCurrentState({ limit: 1 })
      .then((response) => {
        if (cancelled) return;
        setSnapshot({
          key: reloadToken,
          state: { phase: "ready", data: response.completeness, error: null },
        });
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setSnapshot({
          key: reloadToken,
          state: {
            phase: "error",
            data: null,
            error: error instanceof Error ? error.message : String(error),
          },
        });
      });
    return () => {
      cancelled = true;
    };
  }, [reloadToken]);

  const completeness = state.data;
  // The observed subset counts are cohort-wide and the backend withholds them
  // when only part of the cohort has been observed, so a partial read never
  // gets summarised into "0 banned".
  const subset = completeness?.observed_subset_counts ?? null;
  const banned = subset?.banned ?? 0;
  const retryWait = subset?.retry_wait ?? 0;
  const unobserved = completeness?.unobserved_target_count ?? 0;

  return (
    <OperatorSectionCard
      data-testid="routing-health-entry"
      title={copy.routingHealthEntryTitle}
      description={
        completeness && subset ? (
          messages.routingHealth.currentStateSummary(
            formatNumber(completeness.configured_target_count),
            formatNumber(banned),
            formatNumber(retryWait),
          )
        ) : (
          copy.routingHealthEntryDescription
        )
      }
      actions={
        <Button asChild variant="outline" size="sm">
          <Link to="/observe/routing-health">
            {copy.routingHealthEntryAction}
            <ArrowRight data-icon="inline-end" />
          </Link>
        </Button>
      }
    >
      {state.phase === "error" ? (
        <OperatorErrorState
          testId="routing-health-entry-error"
          title={copy.routingHealthStateUnavailable}
          description={messages.honesty.readFailedDescription}
          details={state.error}
          detailsLabel={messages.honesty.viewDetails}
          action={
            <OperatorRetryButton onClick={load}>
              {copy.retry}
            </OperatorRetryButton>
          }
        />
      ) : (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-1.5">
            {state.phase === "loading" || !completeness ? (
              <span className="text-xs text-muted-foreground">
                {copy.routingHealthChecking}
              </span>
            ) : completeness.configured_target_count === 0 ? (
              <>
                <OperatorStatusBadge
                  intent="idle"
                  preserveLabel
                  label={copy.routingHealthNoTargets}
                />
                <span className="text-xs text-muted-foreground">
                  {copy.routingHealthNoTargetsDescription}
                </span>
              </>
            ) : (
              <>
                {/* 异常永远比正常显眼：有封禁就先说封禁，其次等待重试。 */}
                {banned > 0 ? (
                  <OperatorStatusBadge
                    intent="failing"
                    preserveLabel
                    label={copy.routingHealthBannedBadge(formatNumber(banned))}
                  />
                ) : null}
                {retryWait > 0 ? (
                  <OperatorStatusBadge
                    intent="degraded"
                    preserveLabel
                    label={copy.routingHealthRetryWaitBadge(
                      formatNumber(retryWait),
                    )}
                  />
                ) : null}
                {subset && banned === 0 && retryWait === 0 ? (
                  <OperatorStatusBadge
                    intent="healthy"
                    preserveLabel
                    label={copy.routingHealthAllAvailable}
                  />
                ) : null}
                {unobserved > 0 ? (
                  <OperatorStatusBadge
                    intent="idle"
                    preserveLabel
                    label={copy.routingHealthUnobservedBadge(
                      formatNumber(unobserved),
                    )}
                  />
                ) : null}
                {/* 一个都没观测到时，「尚未观测 N」已经说清了；这条只用于
                    观测了一部分、因而封禁与重试计数不可结论的情形。 */}
                {!subset &&
                unobserved < completeness.configured_target_count ? (
                  <OperatorClippedBadge
                    label={copy.routingHealthPartialCounts}
                    reason={copy.routingHealthPartialCountsReason}
                  />
                ) : null}
              </>
            )}
          </div>
          <p className="text-xs text-muted-foreground">
            {copy.routingHealthBestEffortNote}
          </p>
        </div>
      )}
    </OperatorSectionCard>
  );
}
