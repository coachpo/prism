import { describe, expect, it } from "vitest";
import type { AuditLogDetail } from "@/lib/types";
import { comparisonBody, compareRetainedLines } from "./requestComparison";

function capture(overrides: Partial<AuditLogDetail> = {}): AuditLogDetail {
  return {
    request_body_stored: true,
    request_body_base64: btoa('{"model":"a"}'),
    request_body_truncated: false,
    response_body_stored: true,
    response_body_base64: btoa(
      'data: {"delta":{"tool_calls":[{"function":{"arguments":"{\\"q\\":"}}]}}\n\n',
    ),
    response_body_truncated: true,
    response_body_bytes_observed: 100,
    response_body_bytes_stored: 60,
    ...overrides,
  } as AuditLogDetail;
}

describe("retained request comparison", () => {
  it("compares selected direction without reconstructing SSE or tool fragments", () => {
    const record = capture();
    expect(comparisonBody(record, "request").text).toBe('{"model":"a"}');
    const body = comparisonBody(record, "response");
    expect(body.truncated).toBe(true);
    expect(body.text).toBe(atob(record.response_body_base64!));
    expect(compareRetainedLines(body.text!, body.text!).equal).toBe(true);
  });
  it("distinguishes missing, stored empty, invalid base64 and binary", () => {
    expect(
      comparisonBody(capture({ request_body_stored: false }), "request").text,
    ).toBeNull();
    expect(
      comparisonBody(capture({ request_body_base64: "" }), "request").text,
    ).toBe("");
    expect(
      comparisonBody(capture({ request_body_base64: "%%%" }), "request").binary,
    ).toBe(true);
    expect(
      comparisonBody(
        capture({ request_body_base64: btoa(String.fromCharCode(255, 0, 1)) }),
        "request",
      ).binary,
    ).toBe(true);
  });
  it("keeps image references as literal text and bounds only display, not equality", () => {
    const image = '{"image_url":"data:image/png;base64,AAAA"}';
    expect(
      comparisonBody(capture({ request_body_base64: btoa(image) }), "request")
        .text,
    ).toBe(image);
    const prefix = Array.from({ length: 201 }, () => "x".repeat(2001)).join(
      "\n",
    );
    const result = compareRetainedLines(prefix + "A", prefix + "B");
    expect(result.equal).toBe(false);
    expect(result.lines).toHaveLength(200);
    expect(result.lines[0].left).toHaveLength(2000);
  });
});
