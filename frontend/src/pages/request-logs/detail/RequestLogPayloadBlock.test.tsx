import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { RequestLogPayloadBlock } from "./RequestLogPayloadBlock";

function renderContent(content: string, contentKind: "headers" | "payload" = "payload") {
  return render(<LocaleProvider><RequestLogPayloadBlock title="已保存的回复" content={content} contentKind={contentKind} bodyKind="response" apiFamily="openai" /></LocaleProvider>);
}

describe("request content", () => {
  it("shows and copies readable replies without exposing transport fields or raw views", () => {
    renderContent(JSON.stringify({ id: "internal-response-id", choices: [{ message: { role: "assistant", content: "你好，这是一条完整回复。" }, finish_reason: "stop" }], usage: { total_tokens: 42 } }));
    expect(screen.getByText("你好，这是一条完整回复。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "复制" })).toBeEnabled();
    expect(screen.queryByText(/internal-response-id|total_tokens|原始|JSON/)).not.toBeInTheDocument();
  });
  it("does not present unknown error JSON as user conversation", () => {
    renderContent('{"error":{"message":"SQL SELECT /internal/path"}}');
    expect(screen.getByText(/没有可显示的对话内容/)).toBeInTheDocument();
    expect(screen.queryByText(/SQL SELECT/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "复制" })).not.toBeInTheDocument();
  });
  it("keeps headers outside the conversation surface", () => {
    renderContent('{"Authorization":"Bearer secret","Content-Type":"application/json"}', "headers");
    expect(screen.getByText(/连接信息不属于对话内容/)).toBeInTheDocument();
    expect(screen.queryByText(/Bearer secret|Content-Type/)).not.toBeInTheDocument();
  });
});
