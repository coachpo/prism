import { describe, expect, it } from "vitest"
import { applyPreset, BAN_POLICY_PRESETS, DEFAULT_BAN_POLICY_FORM_VALUES, banPolicyFormSchema, banPolicyFormValuesFromStrategy, buildBanPolicyPayload, presetMatchingValues } from "./banPolicySchemas"

function validForm(overrides = {}) {
  return { ...DEFAULT_BAN_POLICY_FORM_VALUES, name: "Explicit Ban Policy", ...overrides }
}

describe("Ban Policy strategy schema", () => {
  it("rejects invalid HTTP status codes before payload creation", () => {
    const result = banPolicyFormSchema.safeParse(validForm({ failure_status_codes_input: "99,600" }))
    expect(result.success).toBe(false)
    if (!result.success) {
      expect(result.error.issues.map((issue) => issue.message).join("\n")).toMatch(/between 100 and 599/)
    }
  })

  it("preserves backend payload fields and normalized status-code ordering", () => {
    expect(buildBanPolicyPayload(validForm({
      name: "  Until reset  ",
      legacy_strategy_type: "round-robin",
      failure_status_codes_input: "503, 429, 500",
      ban_mode: "until_reset",
      cycle_retry_attempt_limit: 3,
      ban_cumulative_retry_attempt_threshold: 6,
      ban_duration_seconds: 0,
    }))).toEqual({
      name: "Until reset",
      legacy_strategy_type: "round-robin",
      failure_status_codes: [429, 500, 503],
      ban_mode: "until_reset",
      retry_base_delay_ms: 60000,
      retry_backoff_multiplier: 2,
      retry_jitter_ratio: 0.2,
      retry_max_delay_ms: 900000,
      cycle_retry_attempt_limit: 3,
      ban_cumulative_retry_attempt_threshold: 6,
      ban_duration_seconds: 0,
    })
  })

  it("maps persisted strategy responses into form values", () => {
    const form = banPolicyFormValuesFromStrategy({ id: 77, profile_id: 71, name: "Temporary policy", legacy_strategy_type: "fill-first", is_default: false, failure_status_codes: [529, 429, 529], ban_mode: "temporary", retry_base_delay_ms: 250, retry_backoff_multiplier: 1.5, retry_jitter_ratio: 0.3, retry_max_delay_ms: 3000, cycle_retry_attempt_limit: 2, ban_cumulative_retry_attempt_threshold: 4, ban_duration_seconds: 120, attached_model_count: 1, created_at: "2026-06-11T00:00:00Z", updated_at: "2026-06-11T00:00:00Z" })
    expect(form.failure_status_codes_input).toBe("429, 529")
    expect(form.ban_mode).toBe("temporary")
    expect(form.cycle_retry_attempt_limit).toBe(2)
    expect(form.ban_cumulative_retry_attempt_threshold).toBe(4)
  })
})

describe("routing strategy presets and provenance", () => {
  it("exposes the three SPEC §5.6 presets with exact full payloads", () => {
    expect(BAN_POLICY_PRESETS.conservative).toMatchObject({
      failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529],
      retry_base_delay_ms: 120000,
      retry_backoff_multiplier: 2,
      retry_jitter_ratio: 0.2,
      retry_max_delay_ms: 1800000,
      cycle_retry_attempt_limit: 2,
      ban_mode: "temporary",
      ban_cumulative_retry_attempt_threshold: 4,
      ban_duration_seconds: 3600,
    })
    expect(BAN_POLICY_PRESETS.balanced).toMatchObject({
      retry_base_delay_ms: 60000,
      retry_max_delay_ms: 900000,
      cycle_retry_attempt_limit: 3,
      ban_mode: "off",
      ban_cumulative_retry_attempt_threshold: 0,
      ban_duration_seconds: 0,
    })
    expect(BAN_POLICY_PRESETS.aggressive).toMatchObject({
      retry_base_delay_ms: 10000,
      retry_backoff_multiplier: 1.5,
      retry_max_delay_ms: 120000,
      cycle_retry_attempt_limit: 5,
      ban_mode: "off",
      ban_cumulative_retry_attempt_threshold: 0,
      ban_duration_seconds: 0,
    })
  })

  it("presets never change the name or routing type", () => {
    const form = validForm({ name: "My Strategy", legacy_strategy_type: "single" })
    const applied = applyPreset(form, BAN_POLICY_PRESETS.aggressive)
    expect(applied.name).toBe("My Strategy")
    expect(applied.legacy_strategy_type).toBe("single")
    expect(applied.retry_base_delay_ms).toBe(10000)
  })

  it("detects exact preset matches and custom combinations", () => {
    const balanced = applyPreset(validForm(), BAN_POLICY_PRESETS.balanced)
    expect(presetMatchingValues(balanced)).toBe("balanced")
    const tweaked = { ...balanced, retry_base_delay_ms: 65000 }
    expect(presetMatchingValues(tweaked)).toBeNull()
  })

  it("normalizes off and until_reset payload fields while keeping the draft", () => {
    // Off submits the normalized 0/0 payload; the draft itself may keep the
    // user's values until they switch back (payload normalization never loses
    // the user's draft in the form).
    const offPayload = buildBanPolicyPayload(validForm({
      ban_mode: "off",
      ban_cumulative_retry_attempt_threshold: 0,
      ban_duration_seconds: 0,
    }))
    expect(offPayload.ban_cumulative_retry_attempt_threshold).toBe(0)
    expect(offPayload.ban_duration_seconds).toBe(0)
    const untilResetPayload = buildBanPolicyPayload(validForm({
      ban_mode: "until_reset",
      ban_cumulative_retry_attempt_threshold: 6,
      ban_duration_seconds: 0,
    }))
    expect(untilResetPayload.ban_duration_seconds).toBe(0)
    expect(untilResetPayload.ban_cumulative_retry_attempt_threshold).toBe(6)
  })
})
