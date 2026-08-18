import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { getStaticMessages } from "@/i18n/staticMessages";
import { RequestLogPayloadBlock } from "./RequestLogPayloadBlock";

const messages = getStaticMessages().requestLogs;

function renderHeaders(content: string) {
  return render(
    <LocaleProvider>
      <RequestLogPayloadBlock title={messages.requestHeaders} content={content} contentKind="headers" />
    </LocaleProvider>,
  );
}

describe("RequestLogPayloadBlock header states", () => {
  it("renders every array entry, including duplicate names, with masking", () => {
    renderHeaders(JSON.stringify([
      { name: "Set-Cookie", value: "session=live-component-secret" },
      { name: "Set-Cookie", value: "session=live-component-secret-2" },
      { name: "Content-Type", value: "application/json" },
    ]));

    expect(screen.getAllByText("set-cookie")).toHaveLength(2);
    expect(screen.getAllByText("[REDACTED]")).toHaveLength(2);
    expect(screen.getByText("application/json")).toBeInTheDocument();
    expect(screen.queryByText("session=live-component-secret")).not.toBeInTheDocument();
    expect(screen.queryByText("session=live-component-secret-2")).not.toBeInTheDocument();
  });

  it("distinguishes zero entries and an absent capture", () => {
    const { unmount } = renderHeaders("[]");
    expect(screen.getByText(messages.headerEmpty(messages.requestHeaders))).toBeInTheDocument();

    unmount();
    renderHeaders("");
    expect(screen.getByText(messages.noCaptured(messages.requestHeaders))).toBeInTheDocument();
  });

  it("renders malformed headers as a degraded state instead of blank content", () => {
    renderHeaders("authorization: Bearer live-component-secret");

    const degraded = screen.getByTestId("request-log-headers-malformed");
    expect(degraded).toHaveTextContent(messages.headerMalformed(messages.requestHeaders));
    expect(degraded).toBeVisible();
  });
});
