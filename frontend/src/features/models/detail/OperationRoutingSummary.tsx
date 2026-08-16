import { useLocale } from "@/i18n/useLocale";
import type { RoutingDiagnosticsResponse, RoutingDiagnosticRoute } from "@/lib/api/observability";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorStatusBadge,
  OperatorTypeBadge,
  type OperatorBadgeIntent,
} from "@/shared/design-system";

type ObserveCopy = ReturnType<typeof useLocale>["messages"]["observe"];

/**
 * Static routing summary (MC-A2/A3/A4): authoritative backend analyzer output
 * for the accepted OpenAI operations plus configuration warnings. The backend
 * is the only source of truth; the frontend never re-derives dispositions.
 */
export function OperationRoutingSummary({ diagnostics }: { diagnostics: RoutingDiagnosticsResponse | null }) {
  const { messages } = useLocale();
  const copy = messages.observe;
  if (!diagnostics) {
    return null;
  }
  const warnings = diagnostics.configuration_warnings ?? [];
  const routes = diagnostics.operation_routes ?? [];
  const singleTruncated = warnings.find((warning) => warning.code === "single_strategy_truncates_targets");

  return (
    <OperatorInsetPanel data-testid="operation-routing-summary">
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <span className="font-medium">{copy.routingSummaryTitle}</span>
          {diagnostics.openai_accepted_format ? (
            <OperatorTypeBadge intent="accent" label={modeLabel(diagnostics.openai_accepted_format, copy)} preserveLabel />
          ) : null}
          {diagnostics.strategy ? (
            <span className="text-xs text-muted-foreground">
              {copy.routingStrategyLabel}: {strategyLabel(diagnostics.strategy.type, copy)}
            </span>
          ) : null}
        </div>
        <ul className="flex flex-col gap-1" data-testid="routing-operation-list">
          {buildOperationGroups(routes).map((group) => (
            <li key={group.key} data-testid={`routing-operation-${group.key}`}>
              <OperationGroupRow group={group} copy={copy} />
            </li>
          ))}
        </ul>
        {singleTruncated ? (
          <OperatorCallout intent="warning" data-testid="single-truncation-callout">
            {singleTruncated.message}
          </OperatorCallout>
        ) : null}
        {warnings
          .filter((warning) => warning && warning.code !== "single_strategy_truncates_targets")
          .map((warning) => (
            <OperatorCallout key={warning.code} intent={warning.severity === "danger" ? "danger" : "warning"}>
              {warning.message}
            </OperatorCallout>
          ))}
      </div>
    </OperatorInsetPanel>
  );
}

/**
 * Visible operation groups, mirroring the authoritative grouping in the backend
 * `modelrouting` package: the three Responses-family operations are one group,
 * and the two image operations are one group. Anything absent from this table
 * (Anthropic, Gemini) keeps one row per registered operation name.
 */
const OPERATION_GROUP_KEYS: Record<string, string> = {
  "openai.chat_completions": "chat_completions",
  "openai.responses": "responses",
  "openai.responses.input_tokens": "responses",
  "openai.responses.compact": "responses",
  "openai.images.generations": "images",
  "openai.images.edits": "images",
};

interface OperationGroup {
  key: string;
  members: RoutingDiagnosticRoute[];
}

/**
 * Buckets the backend routes by visible group, preserving the order the backend
 * emitted: the group takes the position of its first member, so the operation
 * order stays the analyzer's rather than a frontend-local one.
 */
function buildOperationGroups(routes: RoutingDiagnosticRoute[]): OperationGroup[] {
  const groups: OperationGroup[] = [];
  const byKey = new Map<string, OperationGroup>();
  for (const route of routes) {
    const key = OPERATION_GROUP_KEYS[route.operation_name] ?? route.operation_name;
    const existing = byKey.get(key);
    if (existing) {
      existing.members.push(route);
      continue;
    }
    const group: OperationGroup = { key, members: [route] };
    byKey.set(key, group);
    groups.push(group);
  }
  return groups;
}

interface RouteDisposition {
  key: string;
  intent: OperatorBadgeIntent;
  label: string;
}

function routeDisposition(route: RoutingDiagnosticRoute, copy: ObserveCopy): RouteDisposition {
  if (!route.accepted) {
    return { key: "not_accepted", intent: "idle", label: copy.routingNotAccepted };
  }
  if (route.statically_routable) {
    return { key: "routable", intent: "healthy", label: copy.routingRoutable };
  }
  if (route.configured_leaf_exists) {
    return { key: "configured_but_ineligible", intent: "degraded", label: copy.routingConfiguredButIneligible };
  }
  return { key: "uncovered", intent: "failing", label: copy.routingUncovered };
}

function OperationGroupRow({ group, copy }: { group: OperationGroup; copy: ObserveCopy }) {
  const members = group.members.map((route) => ({ route, disposition: routeDisposition(route, copy) }));
  const uniform = members.every((member) => member.disposition.key === members[0].disposition.key);
  return (
    <div className="flex items-center justify-between gap-2 rounded-md px-1 py-0.5 text-sm">
      <span className="font-mono text-xs">{groupLabel(group.key, copy)}</span>
      {uniform ? (
        <OperatorStatusBadge intent={members[0].disposition.intent} label={members[0].disposition.label} />
      ) : (
        // Members of one group are authorized independently: a model may accept
        // generations and not edits. One aggregated badge would report the
        // whole group as routable when only one member is, so a split group
        // stays split, one badge per member.
        <div className="flex flex-wrap items-center justify-end gap-1.5">
          {members.map(({ route, disposition }) => (
            <OperatorStatusBadge
              key={route.operation_name}
              intent={disposition.intent}
              label={copy.routingMemberState(memberLabel(route.operation_name, copy), disposition.label)}
              preserveLabel
            />
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * Non-OpenAI families are not grouped, so their group key is the raw registry
 * operation name. Those dotted identifiers are internal enum keys and must not
 * reach the screen — every registered operation carries a localized label.
 */
function operationLabel(operationName: string, copy: ObserveCopy): string | null {
  if (operationName === "anthropic.messages") return copy.routingAnthropicMessagesLabel;
  if (operationName === "anthropic.count_tokens") return copy.routingAnthropicCountTokensLabel;
  if (operationName === "gemini.generate_content") return copy.routingGeminiGenerateContentLabel;
  if (operationName === "gemini.stream_generate_content") return copy.routingGeminiStreamGenerateContentLabel;
  if (operationName === "gemini.count_tokens") return copy.routingGeminiCountTokensLabel;
  return null;
}

function groupLabel(groupKey: string, copy: ObserveCopy): string {
  if (groupKey === "chat_completions") return copy.routingChatLabel;
  if (groupKey === "responses") return copy.routingResponsesLabel;
  if (groupKey === "images") return copy.routingImagesLabel;
  return operationLabel(groupKey, copy) ?? copy.routingUnknownOperationLabel;
}

/** Short name used only when a group's members disagree and must be told apart. */
function memberLabel(operationName: string, copy: ObserveCopy): string {
  if (operationName === "openai.images.generations") return copy.imagesGenerations;
  if (operationName === "openai.images.edits") return copy.imagesEdits;
  if (operationName.startsWith("openai.")) return operationName.slice("openai.".length);
  return operationLabel(operationName, copy) ?? copy.routingUnknownOperationLabel;
}

function modeLabel(mode: string, copy: ObserveCopy): string {
  if (mode === "dual_native") return copy.modeDual ?? "双模式";
  if (mode === "chat_completions_only") return copy.modeChat ?? "仅 Chat Completions";
  if (mode === "responses_only") return copy.modeResponses ?? "仅 Responses";
  return mode;
}

function strategyLabel(type: string, copy: ObserveCopy): string {
  if (type === "single") return copy.strategySingle ?? "单一";
  if (type === "fill-first") return copy.strategyFillFirst ?? "优先填满";
  if (type === "round-robin") return copy.strategyRoundRobin ?? "轮询";
  return type;
}
