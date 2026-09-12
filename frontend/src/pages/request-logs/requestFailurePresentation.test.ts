import { describe, expect, it } from "vitest";
import { describeRequestFailure, requestServiceLabel } from "./requestFailurePresentation";

describe("request failure recovery", () => {
  it("explains a 404 without claiming which configuration is wrong", () => {
    const result = describeRequestFailure({ statusCode: 404 });
    expect(result?.description).toContain("记录本身不能确定");
    expect(result?.nextStep).toContain("服务地址");
    expect(result?.nextStep).toContain("模型名称");
  });
  it("keeps rate limit and quota causes uncertain, and distinguishes interrupted successful HTTP replies", () => {
    expect(describeRequestFailure({ statusCode: 429 })?.description).toContain("无法区分");
    expect(describeRequestFailure({ statusCode: 200, streamOutcome: "upstream_read_error" })?.title).toBe("回复中断");
    expect(describeRequestFailure({ statusCode: 200, streamOutcome: "completed" })).toBeNull();
    expect(describeRequestFailure({ statusCode: null, streamErrorKind: "future_internal_code" })?.description).toContain("没有足够");
  });
  it("replaces generated internal labels while preserving authored service names", () => {
    expect(requestServiceLabel("Terminal Target #1", "测试服务")).toBe("测试服务");
    expect(requestServiceLabel("我的服务")).toBe("我的服务");
  });
});
