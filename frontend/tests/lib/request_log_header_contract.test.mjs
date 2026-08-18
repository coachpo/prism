import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const repoRoot = path.resolve(frontendDir, "..");
const goldenPath = path.join(
  repoRoot,
  "backend/internal/httpapi/runtime/testdata/audit_header_rows.golden.json",
);
const golden = JSON.parse(readFileSync(goldenPath, "utf8"));

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { buildRequestLogHeaderDocument, formatRequestLogHeaderRaw } = load(
  path.join(frontendDir, "src/pages/request-logs/detail/requestLogPayloadDocuments.ts"),
);

// serialized is byte-for-byte what the backend writes to the audit row, so it is
// also exactly what this parser receives in production. Feeding it back through
// the raw view asserts the round trip preserves the backend array contract.
function assertHeaderOutputMatchesGolden(name, serialized) {
  const backendEntries = JSON.parse(serialized);
  const view = buildRequestLogHeaderDocument(serialized);

  assert.equal(view.kind, "entries", `${name} should parse as header entries`);
  assert.equal(view.entries.length, backendEntries.length, `${name} should preserve every array item`);
  assert.deepEqual(
    JSON.parse(formatRequestLogHeaderRaw(serialized)),
    backendEntries,
    `${name} raw output should remain the backend array contract`,
  );

  const output = JSON.stringify(view) + formatRequestLogHeaderRaw(serialized);
  assert.doesNotMatch(output, /live-(?:golden-request-key|golden-cookie|response-cookie)/);
  return view;
}

test("request and response header golden fixtures stay array-shaped and fully masked", () => {
  const requestView = assertHeaderOutputMatchesGolden("request headers", golden.request_serialized);
  const responseView = assertHeaderOutputMatchesGolden("response headers", golden.response_serialized);

  for (const [name, source] of [
    ["request", golden.request_source],
    ["response", golden.response_source],
  ]) {
    const output = JSON.stringify(buildRequestLogHeaderDocument(source)) + formatRequestLogHeaderRaw(source);
    assert.doesNotMatch(output, /sk-live-golden-request-key|live-golden-cookie|live-response-cookie/);
    assert.equal(JSON.parse(source).length > 0, true, `${name} source should contain live fixture data`);
  }

  assert.equal(
    responseView.entries.filter((entry) => entry.name === "set-cookie").length,
    2,
    "duplicate response headers must remain separate rows",
  );
  assert.equal(
    requestView.entries.find((entry) => entry.name === "authorization")?.value,
    "[REDACTED]",
  );
});

test("header parser exposes empty, absent, and malformed states without compatibility parsing", () => {
  assert.deepEqual(buildRequestLogHeaderDocument("[]"), { kind: "empty" });
  assert.deepEqual(buildRequestLogHeaderDocument("  \n\t"), { kind: "absent" });
  assert.deepEqual(buildRequestLogHeaderDocument('{"authorization":"Bearer live-secret"}'), { kind: "malformed" });
  assert.deepEqual(buildRequestLogHeaderDocument("authorization: Bearer live-secret"), { kind: "malformed" });

  assert.equal(formatRequestLogHeaderRaw("[]"), "[]");
  assert.equal(formatRequestLogHeaderRaw(""), "");
  assert.equal(formatRequestLogHeaderRaw("not-json"), "not-json");
});
