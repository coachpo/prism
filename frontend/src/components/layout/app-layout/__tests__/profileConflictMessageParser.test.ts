import { describe, expect, it } from "vitest";
import { parseConflictMessage } from "../profileConflictMessageParser";

describe("parseConflictMessage", () => {
  it("returns the caller-provided localized max-profile message", () => {
    expect(
      parseConflictMessage(
        new Error("你已达到上限（10）。请删除一个未激活的配置档案后再创建新的配置档案。"),
        "你已达到上限（10）。请删除一个未激活的配置档案后再创建新的配置档案。",
      ),
    ).toBe("你已达到上限（10）。请删除一个未激活的配置档案后再创建新的配置档案。");
  });

  it("passes through generic conflict messages unchanged", () => {
    expect(parseConflictMessage(new Error("409 conflict"), "ignored")).toBe("409 conflict");
  });
});
