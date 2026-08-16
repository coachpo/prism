import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { zhCNMessages } = load(path.join(frontendDir, "src/i18n/messages/zh-CN.ts"));

test("event summary keys map one-to-one to the six real event enums", () => {
  const summary = zhCNMessages.routingHealth.eventSummary;
  for (const key of [
    "retryScheduled",
    "retryExhausted",
    "banned",
    "unbanned",
    "recovered",
    "admissionRejected",
  ]) {
    assert.equal(typeof summary[key], "string", `eventSummary.${key} must exist in zh-CN`);
  }
});

test("dead legacy event keys are absent from the catalog", () => {
  const legacy = zhCNMessages.loadbalanceEvents ?? {};
  for (const key of [
    "eventTypeExtended",
    "eventTypeMaxCooldownStrike",
    "eventTypeNotOpened",
    "eventTypeOpened",
    "eventTypeProbeEligible",
  ]) {
    assert.equal(
      key in legacy,
      false,
      `dead event key ${key} must not exist in the zh-CN catalog`,
    );
  }
});

test("no visible revision strings leak into operator copy", () => {
  const text = JSON.stringify(zhCNMessages);
  assert.equal(text.includes("Revision 0"), false, "visible Revision 0 must not appear");
  assert.equal(text.includes("revision 0"), false, "visible revision 0 must not appear");
});
