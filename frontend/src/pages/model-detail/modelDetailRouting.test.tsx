import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import type {
  Connection,
  DiagnosticsTarget,
  LoadbalanceCurrentStateItem,
  ModelAccessTargetConnectionMutation,
  RoutingDiagnosticsResult,
} from "@/lib/types"
import { classifyOpenAICoverage, openaiAcceptedOperationSet, openaiTargetSupportedOperationSet } from "./classifyOpenAICoverage"
import { OpenAICoverageSummary } from "./OpenAICoverageSummary"
import { TerminalTargetCard } from "./TerminalTargetCard"

function renderWithLocale(node: React.ReactNode) {
  return render(<LocaleProvider>{node}</LocaleProvider>)
}

const connection: Connection = {
  id: 15,
  profile_id: 7,
  api_family: "openai",
  endpoint_id: 1,
  endpoint: { id: 1, profile_id: 7, name: "Primary Upstream", base_url: "https://api.example.invalid/v1", has_api_key: true, api_key_fingerprint: "fp_v1_0123456789ab", api_key_updated_at: "", config_revision: 1, created_at: "", updated_at: "" },
  is_active: true,
  priority: 0,
  name: "Primary",
  auth_type: null,
  custom_headers: { "X-Trace": "abc", "X-Tenant": "t1" },
  openai_text_capability: "chat_completions_only",
  pricing_template_id: 2,
  pricing_template: { id: 2, name: "Default", pricing_unit: "PER_1M", pricing_currency_code: "USD", version: 1 },
  custom_request_parameters: null,
  qps_limit: 10,
  max_in_flight_non_stream: 5,
  max_in_flight_stream: 3,
  created_at: "",
  updated_at: "",
}

const mutationTarget: ModelAccessTargetConnectionMutation = {
  target_type: "connection",
  connection_id: 15,
  position: 0,
  is_enabled: true,
}

function diagnosticsTarget(overrides: Partial<DiagnosticsTarget> = {}): DiagnosticsTarget {
  return {
    access_target_id: 42,
    authored_stage_position: 0,
    enabled_strategy_index: 0,
    connection_id: 15,
    coverage: "full",
    supported_operations: ["openai.chat_completions", "openai.responses", "openai.responses.input_tokens", "openai.responses.compact"],
    unsupported_accepted_operations: [],
    operation_results: [
      { operation_name: "openai.chat_completions", disposition: "candidate", terminal_connection_ids: [15] },
    ],
    ...overrides,
  }
}

describe("classifyOpenAICoverage", () => {
  it("classifies the directional 3x3 matrix", () => {
    const cases: Array<{ format: "dual_native" | "chat_completions_only" | "responses_only"; capability: "dual_native" | "chat_completions_only" | "responses_only"; want: "full" | "partial" | "none" }> = [
      { format: "chat_completions_only", capability: "chat_completions_only", want: "full" },
      { format: "chat_completions_only", capability: "responses_only", want: "none" },
      { format: "chat_completions_only", capability: "dual_native", want: "full" },
      { format: "responses_only", capability: "chat_completions_only", want: "none" },
      { format: "responses_only", capability: "responses_only", want: "full" },
      { format: "responses_only", capability: "dual_native", want: "full" },
      { format: "dual_native", capability: "chat_completions_only", want: "partial" },
      { format: "dual_native", capability: "responses_only", want: "partial" },
      { format: "dual_native", capability: "dual_native", want: "full" },
    ]
    for (const test of cases) {
      const result = classifyOpenAICoverage(test.format, test.capability)
      expect(result.coverage, `${test.format} x ${test.capability}`).toBe(test.want)
    }
  })

  it("groups the responses family into one operation set", () => {
    const responses = openaiTargetSupportedOperationSet("responses_only")
    expect(responses).toEqual(["openai.responses", "openai.responses.input_tokens", "openai.responses.compact"])
    const dual = openaiAcceptedOperationSet("dual_native")
    expect(dual).toHaveLength(4)
  })

  it("reports the missing accepted operations", () => {
    const result = classifyOpenAICoverage("dual_native", "chat_completions_only")
    expect(result.unsupportedAcceptedOperations).toEqual(["openai.responses", "openai.responses.input_tokens", "openai.responses.compact"])
  })
})

describe("TerminalTargetCard", () => {
  it("renders identity, capability, coverage and limits without opening a dialog", () => {
    renderWithLocale(
      <TerminalTargetCard
        stagePosition={1}
        target={mutationTarget}
        connection={connection}
        diagnosticsTarget={diagnosticsTarget({ coverage: "partial", unsupported_accepted_operations: ["openai.responses", "openai.responses.input_tokens", "openai.responses.compact"] })}
        truncatedBySingle={false}
        ownerOpenAIAcceptedFormat="dual_native"
        isReadOnly={false}
        canMoveUp={false}
        canMoveDown={false}
        busy={false}
        runtimeState={undefined}
        runtimeResetting={false}
        onToggle={() => undefined}
        onMoveUp={() => undefined}
        onMoveDown={() => undefined}
        onEdit={() => undefined}
        onDelete={() => undefined}
      />,
    )
    expect(screen.getByText("Primary")).toBeInTheDocument()
    expect(screen.getByText("仅 Chat Completions")).toBeInTheDocument()
    expect(screen.getByText("部分覆盖")).toBeInTheDocument()
    expect(screen.getByText("参与路由")).toBeInTheDocument()
    expect(screen.getByText("终端配置已激活")).toBeInTheDocument()
    expect(screen.getByText("https://api.example.invalid/v1")).toBeInTheDocument()
    expect(screen.getByText("Default")).toBeInTheDocument()
    expect(screen.getByText("QPS 10 · 并发 5 / 3")).toBeInTheDocument()
    expect(screen.getByText("2")).toBeInTheDocument()
    expect(screen.getByText("不会承接：openai.responses、openai.responses.input_tokens、openai.responses.compact")).toBeInTheDocument()
  })

  it("marks rows truncated by single strategy", () => {
    renderWithLocale(
      <TerminalTargetCard
        stagePosition={2}
        target={mutationTarget}
        connection={connection}
        diagnosticsTarget={diagnosticsTarget({ enabled_strategy_index: 1, coverage: "full" })}
        truncatedBySingle={true}
        ownerOpenAIAcceptedFormat="dual_native"
        isReadOnly={false}
        canMoveUp={true}
        canMoveDown={false}
        busy={false}
        runtimeState={undefined}
        runtimeResetting={false}
      />,
    )
    expect(screen.getByText("被 single 截断")).toBeInTheDocument()
  })

  it("renders runtime states: unobserved, no cooldown and banned with reset", () => {
    const { rerender } = renderWithLocale(
      <TerminalTargetCard
        stagePosition={1}
        target={mutationTarget}
        connection={connection}
        diagnosticsTarget={diagnosticsTarget()}
        truncatedBySingle={false}
        ownerOpenAIAcceptedFormat="dual_native"
        isReadOnly={false}
        canMoveUp={false}
        canMoveDown={false}
        busy={false}
        runtimeState={undefined}
        runtimeResetting={false}
      />,
    )
    expect(screen.getByText("本进程尚未观测")).toBeInTheDocument()

    const availableState: LoadbalanceCurrentStateItem = {
      connection_id: 15,
      window_started_at: null,
      window_request_count: 0,
      in_flight_non_stream: 1,
      in_flight_stream: 0,
      cycle_retry_attempts: 0,
      cumulative_retry_attempts: 0,
      next_retry_at: null,
      last_retry_delay_ms: 0,
      ban_mode: "off",
      banned_until_at: null,
      last_failure_kind: null,
      last_success_at: "2026-08-08T12:00:00Z",
      last_success_response_headers_latency_ms: 412,
      state: "available",
      created_at: "2026-08-08T11:00:00Z",
      updated_at: "2026-08-08T12:00:00Z",
    }
    rerender(
      <LocaleProvider>
        <TerminalTargetCard
          stagePosition={1}
          target={mutationTarget}
          connection={connection}
          diagnosticsTarget={diagnosticsTarget()}
          truncatedBySingle={false}
          ownerOpenAIAcceptedFormat="dual_native"
          isReadOnly={false}
          canMoveUp={false}
          canMoveDown={false}
          busy={false}
          runtimeState={availableState}
          runtimeResetting={false}
        />
      </LocaleProvider>,
    )
    expect(screen.getByText("当前无冷却限制")).toBeInTheDocument()
    expect(screen.getByText(/412 ms/)).toBeInTheDocument()
    expect(screen.getByText(/在途/)).toBeInTheDocument()

    const bannedState: LoadbalanceCurrentStateItem = {
      ...availableState,
      ban_mode: "until_reset",
      state: "banned",
      banned_until_at: null,
      next_retry_at: "2026-08-08T13:00:00Z",
      cycle_retry_attempts: 3,
    }
    rerender(
      <LocaleProvider>
        <TerminalTargetCard
          stagePosition={1}
          target={mutationTarget}
          connection={connection}
          diagnosticsTarget={diagnosticsTarget()}
          truncatedBySingle={false}
          ownerOpenAIAcceptedFormat="dual_native"
          isReadOnly={false}
          canMoveUp={false}
          canMoveDown={false}
          busy={false}
          runtimeState={bannedState}
          runtimeResetting={false}
          onResetCooldown={() => undefined}
        />
      </LocaleProvider>,
    )
    expect(screen.getByText("重置冷却")).toBeInTheDocument()
  })

  it("never renders endpoint keys or header values", () => {
    renderWithLocale(
      <TerminalTargetCard
        stagePosition={1}
        target={mutationTarget}
        connection={connection}
        diagnosticsTarget={diagnosticsTarget()}
        truncatedBySingle={false}
        ownerOpenAIAcceptedFormat="dual_native"
        isReadOnly={false}
        canMoveUp={false}
        canMoveDown={false}
        busy={false}
        runtimeState={undefined}
        runtimeResetting={false}
      />,
    )
    expect(screen.queryByText("abc")).not.toBeInTheDocument()
    expect(screen.queryByText("t1")).not.toBeInTheDocument()
  })
})

function diagnosticsFixture(overrides: Partial<RoutingDiagnosticsResult> = {}): RoutingDiagnosticsResult {
  return {
    model_config_id: 7,
    strategy: { id: 3, type: "single" },
    accepted_operations: ["openai.chat_completions", "openai.responses", "openai.responses.input_tokens", "openai.responses.compact"],
    stages: [],
    operation_coverage: [],
    configuration_warnings: [],
    ...overrides,
  }
}

describe("OpenAICoverageSummary", () => {
  it("renders group status and warning classification", () => {
    const diagnostics = diagnosticsFixture({
      operation_coverage: [
        {
          operation_name: "openai.chat_completions",
          accepted: true,
          capability_covered: true,
          statically_routable: true,
          resolved_stage: "terminal_targets",
          compatible_access_target_ids: [42],
          access_target_ids: [42],
        },
        {
          operation_name: "openai.responses",
          accepted: true,
          capability_covered: true,
          statically_routable: false,
          resolved_stage: null,
          compatible_access_target_ids: [43],
          access_target_ids: [43],
        },
        {
          operation_name: "openai.responses.input_tokens",
          accepted: true,
          capability_covered: true,
          statically_routable: false,
          resolved_stage: null,
          compatible_access_target_ids: [43],
          access_target_ids: [43],
        },
        {
          operation_name: "openai.responses.compact",
          accepted: true,
          capability_covered: true,
          statically_routable: false,
          resolved_stage: null,
          compatible_access_target_ids: [43],
          access_target_ids: [43],
        },
      ],
      configuration_warnings: [
        {
          code: "openai_operation_uncovered",
          severity: "danger",
          message: "",
          path: "openai_accepted_format",
          model_config_id: 7,
          access_target_id: null,
          connection_id: null,
          operation_names: ["openai.responses", "openai.responses.input_tokens", "openai.responses.compact"],
          details: { reason: "no_static_eligible_target" },
        },
        {
          code: "openai_target_partial_coverage",
          severity: "warning",
          message: "",
          path: "openai_text_capability",
          model_config_id: 7,
          access_target_id: 43,
          connection_id: 16,
          operation_names: ["openai.chat_completions"],
          details: { stage: "terminal_targets" },
        },
        {
          code: "single_strategy_truncates_targets",
          severity: "warning",
          message: "",
          path: "loadbalance_strategy_id",
          model_config_id: 7,
          access_target_id: null,
          connection_id: null,
          operation_names: [],
          details: { stage: "terminal_targets" },
        },
      ],
    })
    renderWithLocale(<OpenAICoverageSummary diagnostics={diagnostics} loading={false} error={null} />)
    expect(screen.getByText("可路由")).toBeInTheDocument()
    expect(screen.getByText("存在能力但当前不参与")).toBeInTheDocument()
    expect(screen.getByText("由终端目标阶段形成候选")).toBeInTheDocument()
    expect(screen.getByText("存在兼容目标，但以下操作当前没有可参与路由的目标：openai.responses、openai.responses.input_tokens、openai.responses.compact")).toBeInTheDocument()
    expect(screen.getByText("该目标只承接部分入口能力。")).toBeInTheDocument()
    expect(screen.getByText("终端目标阶段选择了 single 策略：只有第一个启用目标会参与路由，其余同行被截断。")).toBeInTheDocument()
  })

  it("shows an error state with retry and never a success tone", () => {
    renderWithLocale(<OpenAICoverageSummary diagnostics={null} loading={false} error="无法验证配置" onRetry={() => undefined} />)
    expect(screen.getByText("无法验证配置")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "重试" })).toBeInTheDocument()
  })
})
