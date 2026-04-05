import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ProxyKeyCreateCard } from "../ProxyKeyCreateCard";

describe("ProxyKeyCreateCard i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized create-card copy", () => {
    render(
      <LocaleProvider>
        <ProxyKeyCreateCard
          authAvailable={true}
          createDisabled={false}
          creatingProxyKey={false}
          handleCreateSubmit={vi.fn()}
          latestGeneratedKey={null}
          proxyKeyLimit={100}
          proxyKeyName=""
          proxyKeyNotes=""
          proxyKeysUsed={4}
          remainingKeys={96}
          setProxyKeyName={vi.fn()}
          setProxyKeyNotes={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("创建代理密钥")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "创建密钥" })).toBeInTheDocument();
    expect(screen.getByText("96 个可用名额剩余。")) .toBeInTheDocument();
  });

  it("exposes stable form names for proxy key create fields", () => {
    render(
      <LocaleProvider>
        <ProxyKeyCreateCard
          authAvailable={true}
          createDisabled={false}
          creatingProxyKey={false}
          handleCreateSubmit={vi.fn()}
          latestGeneratedKey={null}
          proxyKeyLimit={100}
          proxyKeyName="runtime key"
          proxyKeyNotes="rotation slot"
          proxyKeysUsed={4}
          remainingKeys={96}
          setProxyKeyName={vi.fn()}
          setProxyKeyNotes={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByDisplayValue("runtime key")).toHaveAttribute("name", "proxy-key-name");
    expect(screen.getByDisplayValue("rotation slot")).toHaveAttribute("name", "proxy-key-notes");
  });
});
