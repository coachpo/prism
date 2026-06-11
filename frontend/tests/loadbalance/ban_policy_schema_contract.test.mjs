import assert from "node:assert/strict"
import path from "node:path"
import test from "node:test"
import { fileURLToPath } from "node:url"
import { createTsModuleLoader } from "../helpers/loadTsModule.mjs"

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const frontendDir = path.resolve(__dirname, "../..")
const { load } = createTsModuleLoader({ rootDir: frontendDir })
const { DEFAULT_BAN_POLICY_FORM_VALUES, banPolicyFormSchema, buildBanPolicyPayload, banPolicyFormValuesFromStrategy } = load(path.join(frontendDir, "src/features/loadbalance/banPolicySchemas.ts"))

function validForm(overrides = {}) {
  return { ...DEFAULT_BAN_POLICY_FORM_VALUES, name: "Explicit Ban Policy", ...overrides }
}

test("Ban Policy schema rejects invalid HTTP status codes before payload creation", () => {
  const result = banPolicyFormSchema.safeParse(validForm({ failure_status_codes_input: "99,600" }))
  assert.equal(result.success, false)
  assert.match(result.error.issues.map((issue) => issue.message).join("\n"), /between 100 and 599/)
})

test("Ban Policy payload preserves backend field names and normalized status codes", () => {
  const payload = buildBanPolicyPayload(validForm({
    name: "  Until reset  ",
    legacy_strategy_type: "round-robin",
    failure_status_codes_input: "503, 429, 500",
    ban_mode: "until_reset",
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 6,
    ban_duration_seconds: 0,
  }))
  assert.deepEqual(payload, {
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

test("Ban Policy form state maps persisted strategy contract fields", () => {
  const form = banPolicyFormValuesFromStrategy({
    id: 77,
    profile_id: 71,
    name: "Temporary policy",
    legacy_strategy_type: "fill-first",
    failure_status_codes: [529, 429, 529],
    ban_mode: "temporary",
    retry_base_delay_ms: 250,
    retry_backoff_multiplier: 1.5,
    retry_jitter_ratio: 0.3,
    retry_max_delay_ms: 3000,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: 4,
    ban_duration_seconds: 120,
    attached_model_count: 1,
    created_at: "2026-06-11T00:00:00Z",
    updated_at: "2026-06-11T00:00:00Z",
  })
  assert.equal(form.legacy_strategy_type, "fill-first")
  assert.equal(form.failure_status_codes_input, "429, 529")
  assert.equal(form.ban_mode, "temporary")
  assert.equal(form.cycle_retry_attempt_limit, 2)
  assert.equal(form.ban_cumulative_retry_attempt_threshold, 4)
})
