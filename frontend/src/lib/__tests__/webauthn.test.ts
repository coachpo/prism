import { afterEach, describe, expect, it, vi } from "vitest";
import { isWebAuthnSupported } from "../webauthn";

describe("isWebAuthnSupported", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("returns false instead of throwing when window is unavailable", () => {
    vi.stubGlobal("window", undefined);

    expect(isWebAuthnSupported()).toBe(false);
  });

  it("returns true when PublicKeyCredential is available on window", () => {
    vi.stubGlobal("window", {
      PublicKeyCredential: class PublicKeyCredential {},
    });

    expect(isWebAuthnSupported()).toBe(true);
  });
});
