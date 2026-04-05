import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ProfileDialogs } from "../ProfileDialogs";
import { ProfileSwitcherPopover } from "../ProfileSwitcherPopover";

describe("profile shell localization", () => {
  beforeEach(() => {
    localStorage.clear();
    localStorage.setItem("prism.locale", "zh-CN");
  });

  it("renders localized profile switcher copy when the saved locale is Chinese", () => {
    render(
      <LocaleProvider>
        <ProfileSwitcherPopover
          activeProfileName="默认"
          canCreateProfile={true}
          deleteDisabledReason={null}
          editDisabledReason={null}
          filteredProfiles={[
            {
              id: 1,
              name: "默认",
              description: null,
              is_active: true,
              is_default: true,
              is_editable: true,
              version: 1,
              created_at: "",
              deleted_at: null,
              updated_at: "",
            },
          ]}
          hasNoMatches={false}
          hasNoProfiles={false}
          isActivating={false}
          onManageProfiles={vi.fn()}
          onOpenCreateDialog={vi.fn()}
          onOpenDeleteDialog={vi.fn()}
          onOpenEditDialog={vi.fn()}
          onOpenChange={vi.fn()}
          onSelectProfile={vi.fn()}
          open={true}
          profileQuery=""
          profileSearchInputRef={{ current: null }}
          selectedIsActive={true}
          selectedProfileButtonRef={{ current: null }}
          selectedProfileId={1}
          selectedProfileName="默认"
          setProfileQuery={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("选择配置档案")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("搜索配置档案...")).toBeInTheDocument();
    expect(screen.getByText("管理配置档案")).toBeInTheDocument();
    expect(screen.getAllByText("活跃").length).toBeGreaterThan(0);
  });

  it("renders localized dialog copy when the saved locale is Chinese", () => {
    render(
      <LocaleProvider>
        <ProfileDialogs
          activateOpen={true}
          activeProfileName="默认"
          canCreateProfile={true}
          clearDeleteError={vi.fn()}
          createOpen={false}
          deleteConfirmInput=""
          deleteConfirmTarget="删除 暂存"
          deleteError={null}
          deleteOpen={false}
          descriptionInput=""
          editOpen={false}
          hasMismatch={true}
          hasSelectedProfile={true}
          isActivating={false}
          isDeleteConfirmMatch={false}
          isDeleting={false}
          isSaving={false}
          nameInput=""
          onActivate={vi.fn()}
          onCreate={vi.fn()}
          onDelete={vi.fn()}
          onEdit={vi.fn()}
          selectedProfileName="暂存"
          setActivateOpen={vi.fn()}
          setCreateOpen={vi.fn()}
          setDeleteConfirmInput={vi.fn()}
          setDeleteOpen={vi.fn()}
          setDescriptionInput={vi.fn()}
          setEditOpen={vi.fn()}
          setNameInput={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("将“暂存”设为运行时流量的活动配置档案？")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "取消" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "启用" })).toBeInTheDocument();
  });

  it("renders localized delete dialog copy when the saved locale is Chinese", () => {
    render(
      <LocaleProvider>
        <ProfileDialogs
          activateOpen={false}
          activeProfileName="默认"
          canCreateProfile={true}
          clearDeleteError={vi.fn()}
          createOpen={false}
          deleteConfirmInput=""
          deleteConfirmTarget="删除 暂存"
          deleteError={null}
          deleteOpen={true}
          descriptionInput=""
          editOpen={false}
          hasMismatch={true}
          hasSelectedProfile={true}
          isActivating={false}
          isDeleteConfirmMatch={false}
          isDeleting={false}
          isSaving={false}
          nameInput=""
          onActivate={vi.fn()}
          onCreate={vi.fn()}
          onDelete={vi.fn()}
          onEdit={vi.fn()}
          selectedProfileName="暂存"
          setActivateOpen={vi.fn()}
          setCreateOpen={vi.fn()}
          setDeleteConfirmInput={vi.fn()}
          setDeleteOpen={vi.fn()}
          setDescriptionInput={vi.fn()}
          setEditOpen={vi.fn()}
          setNameInput={vi.fn()}
        />
      </LocaleProvider>,
    );

    expect(screen.getByText("删除所选配置档案 暂存。此操作不可撤销。")).toBeInTheDocument();
    expect(screen.getByLabelText("输入 删除 暂存 以确认")).toBeInTheDocument();
  });
});
