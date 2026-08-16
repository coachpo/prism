import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useLocale } from "@/i18n/useLocale";

export type SettingsPendingSave = {
  /** Localized card name, so the count can be explained rather than just shown. */
  label: string;
  save: () => Promise<void> | void;
};

interface SettingsSaveActionProps {
  /** Cards with unsaved edits in the active scope. */
  pending: SettingsPendingSave[];
  saving: boolean;
  /** Localized reason saving is unavailable, or `null` when it is available. */
  blockedReason: string | null;
}

/**
 * One save control for the whole page, in the page header.
 *
 * Each card used to own a save button in its own header, which put the primary
 * action of a settings page in four different places and made "did that save?"
 * a per-card question. When the button is unavailable the reason is stated
 * under it instead of hiding in amber text inside some inner card.
 */
export function SettingsSaveAction({ blockedReason, pending, saving }: SettingsSaveActionProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.settingsPage;
  const count = pending.length;
  const disabled = saving || count === 0 || blockedReason !== null;
  const reason = blockedReason
    ? copy.saveBlockedReason(blockedReason)
    : count === 0
      ? copy.nothingToSave
      : pending.map((entry) => entry.label).join(" · ");

  return (
    <div className="flex flex-col items-start gap-1 sm:items-end">
      <Button
        type="button"
        disabled={disabled}
        aria-describedby="settings-save-reason"
        onClick={() => {
          void (async () => {
            for (const entry of pending) {
              await entry.save();
            }
          })();
        }}
      >
        {saving ? <Spinner aria-hidden="true" data-icon="inline-start" /> : null}
        {saving ? copy.saving : count > 0 ? copy.saveChangesWithCount(formatNumber(count)) : copy.saveChanges}
      </Button>
      <p id="settings-save-reason" className="text-xs text-muted-foreground">
        {reason}
      </p>
    </div>
  );
}
