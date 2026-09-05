import { formatApiFamily } from "@/components/apiFamilyPresentation";
import { useLocale } from "@/i18n/useLocale";
import type { ModelConfigListItem } from "@/lib/types";
import { OperatorCallout, OperatorDestructiveDialog } from "@/shared/design-system";

type Props = {
  deleteTarget: ModelConfigListItem | null;
  onDelete: () => void;
  setDeleteTarget: (model: ModelConfigListItem | null) => void;
  /**
   * 把这个模型作为模型目标的配置。被引用时删除必然 409，与其让操作者
   * 点下去换一条英文 toast，不如先把引用者列出来并挡住确认键。
   */
  referrers?: ModelConfigListItem[];
  /** 删除失败的原因，就地显示而不是弹一条稍纵即逝的 toast。 */
  error?: string | null;
};

export function DeleteModelDialog({
  deleteTarget,
  onDelete,
  setDeleteTarget,
  referrers = [],
  error = null,
}: Props) {
  const { messages } = useLocale();
  const copy = messages.modelsUi;
  const fieldCopy = messages.common;
  const displayName = deleteTarget?.display_name?.trim() || deleteTarget?.model_id || "";
  const blocked = referrers.length > 0;

  return (
    <OperatorDestructiveDialog
      open={!!deleteTarget}
      onOpenChange={(open) => !open && setDeleteTarget(null)}
      title={blocked ? copy.deleteModelBlockedTitle : copy.deleteModel}
      description={
        blocked
          ? copy.deleteModelBlockedDescription
          : copy.deleteModelDescription(deleteTarget?.display_name || deleteTarget?.model_id || "")
      }
      cancelLabel={messages.settingsDialogs.cancel}
      confirmLabel={copy.deleteModel}
      confirmDisabled={blocked}
      onCancel={() => setDeleteTarget(null)}
      onConfirm={onDelete}
    >
      {error ? (
        <OperatorCallout intent="danger" description={error} />
      ) : null}
      {blocked ? (
        <div className="flex flex-col gap-1">
          <p className="text-xs font-medium text-muted-foreground">
            {copy.deleteModelReferrersLabel}
          </p>
          <ul className="flex max-h-40 flex-col gap-0.5 overflow-y-auto text-sm">
            {referrers.map((referrer) => (
              <li key={referrer.id} className="truncate">
                {referrer.display_name?.trim() || referrer.model_id}
                <span className="ml-1 font-mono text-xs text-muted-foreground">
                  {referrer.model_id}
                </span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {deleteTarget ? (
        <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
          <div className="flex flex-col gap-2">
            <p className="text-sm font-medium text-foreground">{messages.settingsDialogs.deletionSummary}</p>
            <div className="flex flex-wrap items-center gap-2">
              <p className="truncate text-sm font-medium text-foreground">{displayName}</p>
              <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                {deleteTarget.model_id}
              </code>
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex min-w-0 flex-col gap-1">
              <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{fieldCopy.apiFamily}</p>
              <p className="truncate text-sm text-foreground">
                {formatApiFamily(deleteTarget.api_family ?? "")}
              </p>
            </div>
            <div className="flex min-w-0 flex-col gap-1">
              <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{copy.accessTargets}</p>
              <p className="truncate text-sm text-foreground">{deleteTarget.access_targets.length}</p>
            </div>
          </div>
        </div>
      ) : null}
    </OperatorDestructiveDialog>
  );
}
