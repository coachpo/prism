import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { DeleteConfirmDialog } from "../dialogs/DeleteConfirmDialog";
import { DeleteRuleConfirmDialog } from "../dialogs/DeleteRuleConfirmDialog";
import { RuleDialog } from "../dialogs/RuleDialog";

describe("settings dialogs i18n", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
    vi.stubGlobal(
      "ResizeObserver",
      class ResizeObserver {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  it("renders localized rule dialog copy", () => {
    render(
      <LocaleProvider>
        <TooltipProvider>
          <RuleDialog
            ruleDialogOpen={true}
            setRuleDialogOpen={vi.fn()}
            editingRule={null}
            ruleForm={{ enabled: true, match_type: "exact", name: "", pattern: "" }}
            setRuleForm={vi.fn()}
            handleSaveRule={vi.fn()}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    expect(screen.getByText("新增规则")).toBeInTheDocument();
    expect(screen.getByText("名称")).toBeInTheDocument();
    expect(screen.getByText("类型")).toBeInTheDocument();
    expect(screen.getByText("规则模式")).toBeInTheDocument();
    expect(screen.getByText("启用")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "保存规则" })).toBeInTheDocument();
  });

  it("renders localized delete-confirm dialog copy", () => {
    render(
      <LocaleProvider>
        <DeleteConfirmDialog
          deleteConfirm={{ type: "requests", days: 7, deleteAll: false }}
          setDeleteConfirm={vi.fn()}
          selectedProfileLabel="Default (#1)"
          deleteConfirmPhrase=""
          setDeleteConfirmPhrase={vi.fn()}
          handleBatchDelete={vi.fn()}
          deleting={false}
          isDeletePhraseValid={false}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("确认删除")).toBeInTheDocument();
    expect(screen.getByText("删除摘要")).toBeInTheDocument();
    expect(screen.getByText("输入 删除 以继续")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeInTheDocument();
  });

  it("renders localized delete-rule dialog copy", () => {
    render(
      <LocaleProvider>
        <DeleteRuleConfirmDialog
          deleteRuleConfirm={{
            id: 1,
            name: "authorization",
            match_type: "exact",
            pattern: "authorization",
            enabled: true,
            is_system: false,
            created_at: "",
            updated_at: "",
          }}
          setDeleteRuleConfirm={vi.fn()}
          handleDeleteRule={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("删除规则")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "删除" })).toBeInTheDocument();
  });
});
