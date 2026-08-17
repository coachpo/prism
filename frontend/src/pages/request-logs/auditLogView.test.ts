import { describe, expect, it } from "vitest";
import {
  auditScopedDurationMs,
  auditScopedStatusCode,
  decodeAuditBodyBase64,
} from "./auditLogView";

describe("auditLogView scoped status and duration", () => {
  it("resolves upstream rows from upstream_status_code and attempt_duration_ms", () => {
    const row = {
      row_kind: "upstream",
      upstream_status_code: 200,
      gateway_status_code: 503,
      legacy_status_code: 500,
      attempt_duration_ms: 42,
      legacy_duration_ms: 7,
    };
    expect(auditScopedStatusCode(row)).toBe(200);
    expect(auditScopedDurationMs(row)).toBe(42);
  });

  it("resolves planning and admission rows from the gateway scope", () => {
    for (const rowKind of ["planning", "admission"]) {
      expect(
        auditScopedStatusCode({
          row_kind: rowKind,
          upstream_status_code: 200,
          gateway_status_code: 429,
          legacy_status_code: 500,
        }),
      ).toBe(429);
      expect(
        auditScopedDurationMs({
          row_kind: rowKind,
          attempt_duration_ms: 42,
          legacy_duration_ms: 7,
        }),
      ).toBe(7);
    }
  });

  it("resolves unknown row kinds from the legacy projection without COALESCE", () => {
    const row = {
      row_kind: "legacy_unknown",
      upstream_status_code: null,
      gateway_status_code: null,
      legacy_status_code: 502,
      attempt_duration_ms: null,
      legacy_duration_ms: 123,
    };
    expect(auditScopedStatusCode(row)).toBe(502);
    expect(auditScopedDurationMs(row)).toBe(123);
  });

  it("keeps null when the scoped value is absent", () => {
    expect(
      auditScopedStatusCode({ row_kind: "upstream", upstream_status_code: null, gateway_status_code: 400, legacy_status_code: null }),
    ).toBeNull();
    expect(
      auditScopedDurationMs({ row_kind: "upstream", attempt_duration_ms: null, legacy_duration_ms: 9 }),
    ).toBeNull();
  });
});

function base64FromBytes(bytes: Uint8Array): string {
  let binary = "";
  bytes.forEach((byte) => {
    binary += String.fromCharCode(byte);
  });
  return btoa(binary);
}

describe("decodeAuditBodyBase64", () => {
  it("decodes utf8 base64 bodies to text", () => {
    const encoded = base64FromBytes(new TextEncoder().encode("request body with 中文"));
    expect(decodeAuditBodyBase64(encoded)).toEqual({ text: "request body with 中文", binary: false });
  });

  it("flags non-UTF-8 bytes as binary instead of emitting mojibake", () => {
    const binary = base64FromBytes(new Uint8Array([0xff, 0xfe, 0x00, 0x61]));
    expect(decodeAuditBodyBase64(binary)).toEqual({ text: null, binary: true });
  });

  it("resolves missing and invalid payloads to not-stored", () => {
    expect(decodeAuditBodyBase64(null)).toEqual({ text: null, binary: false });
    expect(decodeAuditBodyBase64(undefined)).toEqual({ text: null, binary: false });
    expect(decodeAuditBodyBase64("not-base64!")).toEqual({ text: null, binary: true });
  });
});