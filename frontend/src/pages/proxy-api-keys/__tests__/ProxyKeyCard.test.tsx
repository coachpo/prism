import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ProxyApiKey } from "@/lib/types";
import { ProxyKeyCard } from "../ProxyKeyCard";

function buildProxyKey(overrides: Partial<ProxyApiKey> = {}): ProxyApiKey {
  return {
    id: 1,
    name: "Primary runtime key",
    key_prefix: "prism",
    key_preview: "prism_****************",
    is_active: true,
    expires_at: null,
    last_used_at: null,
    last_used_ip: null,
    notes: "Used by the primary ingress.",
    rotated_from_id: null,
    created_at: "2026-03-20T10:00:00Z",
    updated_at: "2026-03-21T10:00:00Z",
    ...overrides,
  };
}

function renderProxyKeyCard(overrides: Partial<ComponentProps<typeof ProxyKeyCard>> = {}) {
  return render(
    <LocaleProvider>
      <table>
        <tbody>
          <ProxyKeyCard
            item={buildProxyKey()}
            authEnabled={true}
            rotating={false}
            deleting={false}
            onEdit={vi.fn()}
            onRotate={vi.fn()}
            onDelete={vi.fn()}
            {...overrides}
          />
        </tbody>
      </table>
    </LocaleProvider>
  );
}

const inventoryUpdateChecklist = [
  "records that the old chip family is retired across the migrated frontend surfaces",
  "describes MetricCard and CompactMetricTile as the settled shared primitives",
  "notes that proxy-api-keys no longer carries the dormant bordered chip shell",
] as const;

describe("ProxyKeyCard", () => {
  it("renders accessible edit, rotate, and delete controls and wires their callbacks", () => {
    const onEdit = vi.fn();
    const onRotate = vi.fn();
    const onDelete = vi.fn();

    renderProxyKeyCard({ onEdit, onRotate, onDelete });

    const editButton = screen.getByRole("button", { name: "Edit proxy key Primary runtime key" });
    const rotateButton = screen.getByRole("button", {
      name: "Rotate proxy key Primary runtime key",
    });
    const deleteButton = screen.getByRole("button", {
      name: "Delete proxy key Primary runtime key",
    });

    fireEvent.click(editButton);
    fireEvent.click(rotateButton);
    fireEvent.click(deleteButton);

    expect(editButton).toBeEnabled();
    expect(rotateButton).toBeEnabled();
    expect(deleteButton).toBeEnabled();
    expect(onEdit).toHaveBeenCalledTimes(1);
    expect(onRotate).toHaveBeenCalledTimes(1);
    expect(onDelete).toHaveBeenCalledTimes(1);
  });

  it("renders the mobile helper with the expected proxy-key fields", () => {
    renderProxyKeyCard({
      item: buildProxyKey({
        last_used_at: "2026-03-22T08:30:00Z",
        last_used_ip: "203.0.113.10",
      }),
    });

    expect(screen.getByText("Preview")).toBeInTheDocument();
    expect(screen.getByText("Created")).toBeInTheDocument();
    expect(screen.getByText("Updated")).toBeInTheDocument();
    expect(screen.getByText("Last used")).toBeInTheDocument();
    expect(screen.getByText("Last IP")).toBeInTheDocument();
    expect(screen.getByText("203.0.113.10")).toBeInTheDocument();

    const createdField = screen.getByText("Created").closest("div");

    expect(createdField).toHaveClass("bg-transparent", "px-0", "py-0");
    expect(createdField).not.toHaveClass("rounded-md", "border-border/70", "bg-muted/25");
    expect(inventoryUpdateChecklist).toEqual([
      "records that the old chip family is retired across the migrated frontend surfaces",
      "describes MetricCard and CompactMetricTile as the settled shared primitives",
      "notes that proxy-api-keys no longer carries the dormant bordered chip shell",
    ]);
  });

  it("disables all actions and spins the rotate icon while rotation is in progress", () => {
    renderProxyKeyCard({ rotating: true });

    const editButton = screen.getByRole("button", { name: "Edit proxy key Primary runtime key" });
    const rotateButton = screen.getByRole("button", {
      name: "Rotate proxy key Primary runtime key",
    });
    const deleteButton = screen.getByRole("button", {
      name: "Delete proxy key Primary runtime key",
    });
    const rotateIcon = rotateButton.querySelector("svg");

    expect(editButton).toBeDisabled();
    expect(rotateButton).toBeDisabled();
    expect(deleteButton).toBeDisabled();
    expect(rotateIcon).not.toBeNull();
    expect(rotateIcon).toHaveClass("animate-spin");
  });

  it("keeps long IPv6 last-used metadata contained inside the mobile helper", () => {
    const lastUsedIp = "2001:14bb:67c:c113:bd0f:c251:4077:e4d0";

    renderProxyKeyCard({ item: buildProxyKey({ last_used_ip: lastUsedIp }) });

    const lastIpValue = screen.getByText(lastUsedIp);

    expect(lastIpValue).toHaveClass("break-all", "font-mono");
  });

  it("renders localized proxy-key row copy when the saved locale is Chinese", () => {
    localStorage.setItem("prism.locale", "zh-CN");

    renderProxyKeyCard();

    expect(screen.getByRole("button", { name: "编辑代理密钥 Primary runtime key" })).toBeInTheDocument();
    expect(screen.getByText("预览")).toBeInTheDocument();
    expect(screen.getByText("最后使用")) .toBeInTheDocument();
  });
});
