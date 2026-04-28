import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { resolveRequestAuditCaptureMode } = load(
  path.join(frontendDir, "src/pages/request-logs/requestLogAuditState.ts"),
);

test("request audit capture mode follows request-time provenance booleans", () => {
  assert.equal(
    resolveRequestAuditCaptureMode({ audit_enabled_at_request: false, audit_capture_bodies_at_request: false }),
    "disabled",
  );
  assert.equal(
    resolveRequestAuditCaptureMode({ audit_enabled_at_request: true, audit_capture_bodies_at_request: false }),
    "metadata_only",
  );
  assert.equal(
    resolveRequestAuditCaptureMode({ audit_enabled_at_request: true, audit_capture_bodies_at_request: true }),
    "full",
  );
});
