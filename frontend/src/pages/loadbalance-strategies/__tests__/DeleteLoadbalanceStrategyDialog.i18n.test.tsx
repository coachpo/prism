import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { DeleteLoadbalanceStrategyDialog } from "../DeleteLoadbalanceStrategyDialog";

describe("DeleteLoadbalanceStrategyDialog i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized delete dialog copy", () => {
    render(
      <LocaleProvider>
        <DeleteLoadbalanceStrategyDialog
          deleteLoadbalanceStrategyConfirm={{
            id: 1,
            profile_id: 1,
            name: "demo",
            strategy_type: "legacy",
            legacy_strategy_type: "single",
            auto_recovery: { mode: "disabled" },
            attached_model_count: 0,
            created_at: "",
            updated_at: "",
          }}
          loadbalanceStrategyDeleting={false}
          onClose={vi.fn()}
          onDelete={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByRole("heading", { name: "删除负载均衡策略" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeInTheDocument();
  });
});
