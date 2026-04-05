import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { useLocale } from "@/i18n/useLocale";

function Probe() {
  const { locale } = useLocale();
  return <div>{locale}</div>;
}

describe("LocaleProvider", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.lang = "en";
    Object.defineProperty(window.navigator, "language", {
      configurable: true,
      value: "en-US",
    });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("prefers a saved locale and updates document lang", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    render(
      <LocaleProvider>
        <Probe />
      </LocaleProvider>,
    );

    expect(screen.getByText("zh-CN")).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("zh-CN");
  });

  it("falls back to the browser locale when no saved locale exists", () => {
    Object.defineProperty(window.navigator, "language", {
      configurable: true,
      value: "zh-CN",
    });

    render(
      <LocaleProvider>
        <Probe />
      </LocaleProvider>,
    );

    expect(screen.getByText("zh-CN")).toBeInTheDocument();
  });
});
