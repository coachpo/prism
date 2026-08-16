import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import type {
  Connection,
  DiagnosticsTarget,
  LoadbalanceCurrentStateItem,
  ModelAccessTargetConnectionMutation,
} from "@/lib/types"
import { classifyOpenAICoverage, openaiAcceptedOperationSet, openaiTargetSupportedOperationSet } from "./classifyOpenAICoverage"
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
  openai_image_capability: null,
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
