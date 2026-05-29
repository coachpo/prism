import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const validationMessages = {
  nameRequired: "nameRequired",
  addStatusCode: "addStatusCode",
  statusCodesUnique: "statusCodesUnique",
  statusCodesValidHttp: "statusCodesValidHttp",
  statusCodeIntegerRange: "statusCodeIntegerRange",
  statusCodeExists: "statusCodeExists",
  retryBaseDelayIntegerMs: "retryBaseDelayIntegerMs",
  retryBaseDelayRange: "retryBaseDelayRange",
  backoffMultiplierRange: "backoffMultiplierRange",
  retryJitterRatioRange: "retryJitterRatioRange",
  retryMaxDelayIntegerMs: "retryMaxDelayIntegerMs",
  retryMaxDelayRange: "retryMaxDelayRange",
  cycleRetryAttemptLimitInteger: "cycleRetryAttemptLimitInteger",
  cycleRetryAttemptLimitRange: "cycleRetryAttemptLimitRange",
  banCumulativeRetryAttemptThresholdInteger: "banCumulativeRetryAttemptThresholdInteger",
  banCumulativeRetryAttemptThresholdRange: "banCumulativeRetryAttemptThresholdRange",
  banCumulativeRetryAttemptThresholdMinCycle: "banCumulativeRetryAttemptThresholdMinCycle",
  banDurationIntegerSeconds: "banDurationIntegerSeconds",
  banDurationTemporaryMin: "banDurationTemporaryMin",
  banModeOffThresholdZero: "banModeOffThresholdZero",
  banDurationUntilResetZero: "banDurationUntilResetZero",
};

const staticMessagesStub = {
  getStaticMessages: () => ({
    loadbalanceStrategyValidation: validationMessages,
  }),
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "@/i18n/staticMessages": staticMessagesStub,
  },
});
const {
  DEFAULT_LOADBALANCE_STRATEGY_FORM,
  getLoadbalanceStrategyFormValidationError,
  loadbalanceStrategyFormStateFromStrategy,
  setLoadbalanceStrategyBanMode,
  setLoadbalanceStrategyCycleRetryAttemptLimit,
  toLoadbalanceStrategyPayload,
} = load(path.join(frontendDir, "src/pages/loadbalance-strategies/loadbalanceStrategyFormState.ts"));
const removedRetryAttemptsKey = ["retry", "max", "attempts"].join("_");

function buildForm(overrides = {}) {
  return {
    ...DEFAULT_LOADBALANCE_STRATEGY_FORM,
    failure_status_codes: [...DEFAULT_LOADBALANCE_STRATEGY_FORM.failure_status_codes],
    name: "Policy",
    ...overrides,
  };
}

test("default strategy form keeps persisted Ban Policy defaults explicit", () => {
  assert.equal(DEFAULT_LOADBALANCE_STRATEGY_FORM.cycle_retry_attempt_limit, 3);
  assert.equal(DEFAULT_LOADBALANCE_STRATEGY_FORM.ban_cumulative_retry_attempt_threshold, 0);
  assert.equal(DEFAULT_LOADBALANCE_STRATEGY_FORM.ban_mode, "off");
  assert.equal(DEFAULT_LOADBALANCE_STRATEGY_FORM.ban_duration_seconds, 0);
  assert.ok(!Object.hasOwn(DEFAULT_LOADBALANCE_STRATEGY_FORM, removedRetryAttemptsKey));
});

test("strategy form state maps canonical Ban Policy fields from strategy responses", () => {
  const form = loadbalanceStrategyFormStateFromStrategy({
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

  assert.deepEqual(form.failure_status_codes, [429, 503]);
  assert.equal(form.cycle_retry_attempt_limit, 2);
  assert.equal(form.ban_cumulative_retry_attempt_threshold, 4);
  assert.equal(form.ban_mode, "until_reset");
  assert.ok(!Object.hasOwn(form, removedRetryAttemptsKey));
});

test("form payload emits canonical cycle and threshold fields only", () => {
  const payload = toLoadbalanceStrategyPayload(buildForm({
    name: "  Until reset routing  ",
    legacy_strategy_type: "fill-first",
    failure_status_codes: [503, 429, 503],
    ban_mode: "until_reset",
    retry_base_delay_ms: 60.9,
    retry_max_delay_ms: 900.9,
    cycle_retry_attempt_limit: 2.9,
    ban_cumulative_retry_attempt_threshold: 4.9,
    ban_duration_seconds: 25,
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
  const error = getLoadbalanceStrategyFormValidationError(buildForm({
    ban_mode: "off",
    ban_cumulative_retry_attempt_threshold: 1,
  }));

  assert.equal(error, validationMessages.banModeOffThresholdZero);
});

test("validation requires enabled thresholds to meet or exceed cycle limit", () => {
  const belowCycle = getLoadbalanceStrategyFormValidationError(buildForm({
    ban_mode: "until_reset",
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 2,
  }));
  const zeroThreshold = getLoadbalanceStrategyFormValidationError(buildForm({
    ban_mode: "temporary",
    ban_cumulative_retry_attempt_threshold: 0,
    ban_duration_seconds: 1,
  }));
  const validUntilReset = getLoadbalanceStrategyFormValidationError(buildForm({
    ban_mode: "until_reset",
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 3,
    ban_duration_seconds: 0,
  }));

  assert.equal(belowCycle, validationMessages.banCumulativeRetryAttemptThresholdMinCycle);
  assert.equal(zeroThreshold, validationMessages.banCumulativeRetryAttemptThresholdRange);
  assert.equal(validUntilReset, null);
});

test("validation keeps duration rules tied to ban mode", () => {
  const temporaryTooShort = getLoadbalanceStrategyFormValidationError(buildForm({
    ban_mode: "temporary",
    ban_cumulative_retry_attempt_threshold: 6,
    ban_duration_seconds: 0,
  }));
  const untilResetWithDuration = getLoadbalanceStrategyFormValidationError(buildForm({
    ban_mode: "until_reset",
    ban_cumulative_retry_attempt_threshold: 6,
    ban_duration_seconds: 60,
  }));

  assert.equal(temporaryTooShort, validationMessages.banDurationTemporaryMin);
  assert.equal(untilResetWithDuration, validationMessages.banDurationUntilResetZero);
});

test("switching from off to enabled seeds threshold only from zero", () => {
  const untilReset = setLoadbalanceStrategyBanMode(buildForm({
    ban_mode: "off",
    cycle_retry_attempt_limit: 4,
    ban_cumulative_retry_attempt_threshold: 0,
  }), "until_reset");
  const temporary = setLoadbalanceStrategyBanMode(buildForm({
    ban_mode: "off",
    cycle_retry_attempt_limit: 5,
    ban_cumulative_retry_attempt_threshold: 9,
    ban_duration_seconds: 0,
  }), "temporary");

  assert.equal(untilReset.ban_cumulative_retry_attempt_threshold, 8);
  assert.equal(untilReset.ban_duration_seconds, 0);
  assert.equal(temporary.ban_cumulative_retry_attempt_threshold, 9);
  assert.equal(temporary.ban_duration_seconds, 1);
});

test("switching to off zeros threshold and duration", () => {
  const off = setLoadbalanceStrategyBanMode(buildForm({
    ban_mode: "temporary",
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 6,
    ban_duration_seconds: 60,
  }), "off");

  assert.equal(off.ban_cumulative_retry_attempt_threshold, 0);
  assert.equal(off.ban_duration_seconds, 0);
});

test("raising cycle limit clamps only non-zero lower thresholds", () => {
  const clamped = setLoadbalanceStrategyCycleRetryAttemptLimit(buildForm({
    ban_mode: "until_reset",
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 4,
  }), 5);
  const preservedZero = setLoadbalanceStrategyCycleRetryAttemptLimit(buildForm({
    ban_mode: "off",
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 0,
  }), 5);

  assert.equal(clamped.cycle_retry_attempt_limit, 5);
  assert.equal(clamped.ban_cumulative_retry_attempt_threshold, 5);
  assert.equal(preservedZero.cycle_retry_attempt_limit, 5);
  assert.equal(preservedZero.ban_cumulative_retry_attempt_threshold, 0);
});
