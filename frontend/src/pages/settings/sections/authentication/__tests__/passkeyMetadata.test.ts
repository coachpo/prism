import { beforeEach, describe, expect, it } from "vitest";
import { buildPasskeyMetadata, getPasskeyStateBadge } from "../passkeyMetadata";

describe("passkeyMetadata i18n", () => {
  beforeEach(() => {
    document.documentElement.lang = "zh-CN";
  });

  it("returns localized badge labels", () => {
    expect(
      getPasskeyStateBadge({
        id: 1,
        device_name: null,
        backup_eligible: true,
        backup_state: false,
        created_at: "2026-03-20T10:00:00Z",
        last_used_at: null,
      })?.label,
    ).toBe("可备份");
  });

  it("builds localized passkey metadata strings", () => {
    expect(
      buildPasskeyMetadata({
        id: 1,
        device_name: null,
        backup_eligible: false,
        backup_state: false,
        created_at: "2026-03-20T10:00:00Z",
        last_used_at: null,
      }),
    ).toContain("创建于");
  });
});
