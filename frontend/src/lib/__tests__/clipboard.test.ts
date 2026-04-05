import { afterEach, describe, expect, it, vi } from "vitest";
import { copyTextToClipboard } from "../clipboard";

const originalClipboard = navigator.clipboard;
const originalExecCommand = document.execCommand;

function setClipboard(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
}

function setExecCommand(result: boolean) {
  const execCommand = vi.fn().mockReturnValue(result);

  Object.defineProperty(document, "execCommand", {
    configurable: true,
    value: execCommand,
  });

  return execCommand;
}

afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";

  if (originalClipboard) {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: originalClipboard,
    });
  } else {
    Reflect.deleteProperty(navigator, "clipboard");
  }

  if (originalExecCommand) {
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: originalExecCommand,
    });
  } else {
    Reflect.deleteProperty(document, "execCommand");
  }
});

describe("copyTextToClipboard", () => {
  it("uses navigator.clipboard.writeText when the async clipboard API is available", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const execCommand = setExecCommand(true);
    setClipboard(writeText);

    await expect(copyTextToClipboard("copied value")).resolves.toBe(true);

    expect(writeText).toHaveBeenCalledWith("copied value");
    expect(execCommand).not.toHaveBeenCalled();
  });

  it("falls back to document.execCommand copy when writeText rejects", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("clipboard denied"));
    const execCommand = setExecCommand(true);
    setClipboard(writeText);

    await expect(copyTextToClipboard("fallback value")).resolves.toBe(true);

    expect(writeText).toHaveBeenCalledWith("fallback value");
    expect(execCommand).toHaveBeenCalledWith("copy");
    expect(document.querySelector("textarea")).toBeNull();
  });
});
