import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { resolveSelectedProfile } = load(
  path.join(frontendDir, "src/context/profile/selection.ts"),
);

function createProfile(id, overrides = {}) {
  return {
    id,
    name: `Profile ${id}`,
    description: null,
    is_active: false,
    is_default: false,
    is_editable: true,
    version: 1,
    created_at: "2026-04-28T00:00:00Z",
    deleted_at: null,
    updated_at: "2026-04-28T00:00:00Z",
    ...overrides,
  };
}

test("profile selection keeps a valid stored profile ahead of active and default fallbacks", () => {
  const profiles = [
    createProfile(1, { is_default: true }),
    createProfile(2, { is_active: true }),
    createProfile(3),
  ];

  assert.equal(resolveSelectedProfile(profiles, 3, 2)?.id, 3);
});

test("profile selection prefers the active profile when stored selection is absent or stale", () => {
  const profiles = [
    createProfile(1, { is_default: true }),
    createProfile(2, { is_active: true }),
    createProfile(3),
  ];

  for (const storedProfileId of [null, 999]) {
    assert.equal(
      resolveSelectedProfile(profiles, storedProfileId, 2)?.id,
      2,
      `storedProfileId=${storedProfileId} should fall back to the active profile first`,
    );
  }
});

test("profile selection falls back to the default profile when the active profile is missing", () => {
  const profiles = [
    createProfile(1, { is_default: true }),
    createProfile(2),
  ];

  assert.equal(resolveSelectedProfile(profiles, 999, 777)?.id, 1);
});

test("profile selection falls back to the first profile when stored, active, and default are unavailable", () => {
  const profiles = [createProfile(4), createProfile(7)];

  assert.equal(resolveSelectedProfile(profiles, 999, 777)?.id, 4);
  assert.equal(resolveSelectedProfile([], null, null), null);
});
