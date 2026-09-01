import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { zhCNMessages } from "@/i18n/messages";
import { UpstreamModelIdValue } from "./UpstreamModelIdValue";

it("renders retained upstream identity and an explained historical gap", () => {
  const missingReason = zhCNMessages.requestLogs.upstreamModelIdMissing;
  render(
    <LocaleProvider>
      <UpstreamModelIdValue
        value="provider/Model-A"
        missingReason={missingReason}
      />
      <UpstreamModelIdValue value={null} missingReason={missingReason} />
    </LocaleProvider>,
  );
  expect(screen.getByText("provider/Model-A")).toBeInTheDocument();
  expect(screen.getByTitle(missingReason)).toHaveTextContent("—");
});
