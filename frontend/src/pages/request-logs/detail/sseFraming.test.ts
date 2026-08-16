import { describe, expect, it } from "vitest";
import { frameSseEvents, isSseContent } from "./sseFraming";

describe("frameSseEvents", () => {
  it("parses LF/CRLF/CR-only line endings with a leading BOM", () => {
    const result = frameSseEvents("\uFEFFdata: {\"a\":1}\r\n\r\ndata: {\"b\":2}\rdata: {\"c\":3}\n\n");
    expect(result.events).toHaveLength(2);
    expect(result.events[0].data.trim()).toBe("{\"a\":1}");
    // CR and LF are equivalent line terminators: the two data lines belong
    // to the same event and merge into one multi-line payload.
    expect(result.events[1].data.trim()).toBe("{\"b\":2}\n{\"c\":3}");
    expect(result.events.every((event) => event.terminated)).toBe(true);
    expect(result.hasIncompleteTail).toBe(false);
  });

  it("parses comments, named events and multi-line data", () => {
    const result = frameSseEvents(": ping\nevent: message\ndata: line1\ndata: line2\n\n");
    expect(result.events).toHaveLength(1);
    expect(result.events[0].eventName).toBe("message");
    expect(result.events[0].data.trim()).toBe("line1\nline2");
  });

  it("reports a trailing event without a final blank line as an incomplete tail", () => {
    const result = frameSseEvents("data: {\"a\":1}");
    expect(result.events).toHaveLength(0);
    expect(result.hasIncompleteTail).toBe(true);
    expect(result.incompleteTail).toContain("data:");
  });

  it("parses [DONE] and malformed JSON with per-event errors", () => {
    const result = frameSseEvents('data: {"a":1}\n\ndata: [DONE]\n\ndata: {bad\n\n');
    expect(result.events).toHaveLength(3);
    expect(result.events[0].json).toEqual({ a: 1 });
    expect(result.events[1].data.trim()).toBe("[DONE]");
    expect(result.events[1].json).toBeNull();
    expect(result.events[2].json).toBeNull();
    expect(result.events[2].jsonError).not.toBeNull();
  });
});

describe("isSseContent", () => {
  it("detects SSE-shaped content", () => {
    expect(isSseContent("data: hello\n\n")).toBe(true);
    expect(isSseContent("event: message\ndata: x\n\n")).toBe(true);
    expect(isSseContent("plain text")).toBe(false);
    expect(isSseContent("")).toBe(false);
  });
});
