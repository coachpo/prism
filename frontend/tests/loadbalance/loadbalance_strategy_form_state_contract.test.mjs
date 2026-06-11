import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const {
  DEFAULT_BAN_POLICY_FORM_VALUES,
  banPolicyFormSchema,
  banPolicyFormValuesFromStrategy,
  buildBanPolicyPayload,
} = load(path.join(frontendDir, "src/features/loadbalance/banPolicySchemas.ts"));

const removedRetryAttemptsKey = ["retry", "max", "attempts"].join("_");

function buildForm(overrides = {}) {
  return {
    ...DEFAULT_BAN_POLICY_FORM_VALUES,
    name: "Policy",
    ...overrides,
  };
}

function firstValidationMessage(form) {
  const result = banPolicyFormSchema.safeParse(form);
  return result.success ? null : result.error.issues[0]?.message ?? null;
}

test("default strategy form keeps persisted Ban Policy defaults explicit", () => {
  assert.equal(DEFAULT_BAN_POLICY_FORM_VALUES.cycle_retry_attempt_limit, 3);
  assert.equal(DEFAULT_BAN_POLICY_FORM_VALUES.ban_cumulative_retry_attempt_threshold, 0);
  assert.equal(DEFAULT_BAN_POLICY_FORM_VALUES.ban_mode, "off");
  assert.equal(DEFAULT_BAN_POLICY_FORM_VALUES.ban_duration_seconds, 0);
  assert.equal(DEFAULT_BAN_POLICY_FORM_VALUES.failure_status_codes_input, "403, 422, 429, 500, 502, 503, 504, 529");
  assert.ok(!Object.hasOwn(DEFAULT_BAN_POLICY_FORM_VALUES, removedRetryAttemptsKey));
});

test("strategy form state maps canonical Ban Policy fields from strategy responses", () => {
  const form = banPolicyFormValuesFromStrategy({
    id: 42,
    profile_id: 7,
    name: "Existing",
    legacy_strategy_type: "round-robin",
    failure_status_codes: [503, 429, 503],
    ban_mode: "until_reset",
    retry_base_delay_ms: 1000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.1,
    retry_max_delay_ms: 30000,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: 4,
    ban_duration_seconds: 0,
    attached_model_count: 0,
    created_at: "2026-05-29T00:00:00Z",
    updated_at: "2026-05-29T00:00:00Z",
  });

  assert.equal(form.failure_status_codes_input, "429, 503");
  assert.equal(form.cycle_retry_attempt_limit, 2);
  assert.equal(form.ban_cumulative_retry_attempt_threshold, 4);
  assert.equal(form.ban_mode, "until_reset");
  assert.ok(!Object.hasOwn(form, removedRetryAttemptsKey));
});

test("strategy form state accepts cheapest_eligible_context strategy responses", () => {
  const form = banPolicyFormValuesFromStrategy({
    id: 43,
    profile_id: 7,
    name: "Cheapest",
    legacy_strategy_type: "cheapest_eligible_context",
    failure_status_codes: [500, 429],
    ban_mode: "temporary",
    retry_base_delay_ms: 1000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.1,
    retry_max_delay_ms: 30000,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: 4,
    ban_duration_seconds: 60,
    attached_model_count: 0,
    created_at: "2026-05-29T00:00:00Z",
    updated_at: "2026-05-29T00:00:00Z",
  });

  assert.equal(form.legacy_strategy_type, "cheapest_eligible_context");
  assert.equal(form.failure_status_codes_input, "429, 500");
});

test("form payload emits canonical cycle and threshold fields only", () => {
  const payload = buildBanPolicyPayload(buildForm({
    name: "  Until reset routing  ",
    legacy_strategy_type: "fill-first",
    failure_status_codes_input: "503, 429",
    ban_mode: "until_reset",
    retry_base_delay_ms: 60,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: 4,
    ban_duration_seconds: 0,
  }));

  assert.deepEqual(payload, {
    name: "Until reset routing",
    legacy_strategy_type: "fill-first",
    failure_status_codes: [429, 503],
    ban_mode: "until_reset",
    retry_base_delay_ms: 60,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: 4,
    ban_duration_seconds: 0,
  });
  assert.ok(!Object.hasOwn(payload, removedRetryAttemptsKey));
});

test("validation requires threshold zero when ban mode is off", () => {
  const message = firstValidationMessage(buildForm({
    ban_mode: "off",
    ban_cumulative_retry_attempt_threshold: 1,
  }));

  assert.equal(message, "Ban mode Off requires cumulative threshold 0");
});

test("validation requires enabled thresholds to meet or exceed cycle limit", () => {
  const belowCycle = firstValidationMessage(buildForm({
    ban_mode: "until_reset",
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 2,
  }));
  const zeroThreshold = firstValidationMessage(buildForm({
    ban_mode: "temporary",
    ban_cumulative_retry_attempt_threshold: 0,
    ban_duration_seconds: 1,
  }));
  const validUntilReset = firstValidationMessage(buildForm({
    ban_mode: "until_reset",
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 3,
    ban_duration_seconds: 0,
  }));

  assert.equal(
    belowCycle,
    "Ban cumulative retry attempt threshold must be greater than or equal to the cycle retry attempt limit",
  );
  assert.equal(
    zeroThreshold,
    "Ban cumulative retry attempt threshold must be between 1 and 500 when banning is enabled",
  );
  assert.equal(validUntilReset, null);
});

test("validation keeps duration rules tied to ban mode", () => {
  const temporaryTooShort = firstValidationMessage(buildForm({
    ban_mode: "temporary",
    ban_cumulative_retry_attempt_threshold: 6,
    ban_duration_seconds: 0,
  }));
  const untilResetWithDuration = firstValidationMessage(buildForm({
    ban_mode: "until_reset",
    ban_cumulative_retry_attempt_threshold: 6,
    ban_duration_seconds: 60,
  }));

  assert.equal(temporaryTooShort, "Ban duration must be at least 1 second for temporary bans");
  assert.equal(untilResetWithDuration, "Ban duration must be 0 seconds for off or until-reset bans");
});
