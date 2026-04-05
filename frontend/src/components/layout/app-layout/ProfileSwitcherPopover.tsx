import { type RefObject } from "react";
import { ChevronsUpDown, Check, Pencil, Trash2 } from "lucide-react";
import type { Profile } from "@/lib/types";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useLocale } from "@/i18n/useLocale";

interface ProfileSwitcherPopoverProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  isActivating: boolean;
  selectedProfileName: string;
  activeProfileName: string;
  hasNoProfiles: boolean;
  selectedIsActive: boolean;
  profileQuery: string;
  setProfileQuery: (value: string) => void;
  selectedProfileId: number | null;
  filteredProfiles: Profile[];
  hasNoMatches: boolean;
  canCreateProfile: boolean;
  editDisabledReason: string | null;
  deleteDisabledReason: string | null;
  selectedProfileButtonRef: RefObject<HTMLButtonElement | null>;
  profileSearchInputRef: RefObject<HTMLInputElement | null>;
  onSelectProfile: (profileId: number) => void;
  onOpenEditDialog: () => void;
  onOpenDeleteDialog: () => void;
  onOpenCreateDialog: () => void;
  onManageProfiles: () => void;
}

export function ProfileSwitcherPopover({
  open,
  onOpenChange,
  isActivating,
  selectedProfileName,
  activeProfileName,
  hasNoProfiles,
  selectedIsActive,
  profileQuery,
  setProfileQuery,
  selectedProfileId,
  filteredProfiles,
  hasNoMatches,
  canCreateProfile,
  editDisabledReason,
  deleteDisabledReason,
  selectedProfileButtonRef,
  profileSearchInputRef,
  onSelectProfile,
  onOpenEditDialog,
  onOpenDeleteDialog,
  onOpenCreateDialog,
  onManageProfiles,
}: ProfileSwitcherPopoverProps) {
  const { messages } = useLocale();

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          className="h-9 w-full justify-between gap-2 px-2.5 sm:w-[320px]"
          disabled={isActivating}
          role="combobox"
          aria-expanded={open}
          title={messages.profiles.profileTriggerTitle(selectedProfileName, activeProfileName)}
        >
          <span className="flex min-w-0 items-center gap-2 truncate text-sm">
            {hasNoProfiles ? <span className="h-2 w-2 shrink-0 rounded-full bg-amber-500/80" /> : null}
            <span className="text-muted-foreground">{messages.shell.profile}</span>
            <span className="truncate font-medium">{selectedProfileName}</span>
          </span>

          <span className="flex items-center gap-1.5">
            {selectedIsActive ? <Badge className="h-5 px-1.5 text-[10px]">{messages.profiles.active}</Badge> : null}
            <ChevronsUpDown className="h-4 w-4 opacity-60" />
          </span>
        </Button>
      </PopoverTrigger>

      <PopoverContent
        align="end"
        collisionPadding={8}
        onOpenAutoFocus={(event) => event.preventDefault()}
        className="z-[60] flex h-[min(82vh,34rem)] w-[var(--radix-popover-trigger-width)] max-w-[94vw] flex-col overflow-hidden p-0"
      >
        <div className="shrink-0 border-b px-3 py-3">
          <p className="text-sm font-semibold">{messages.profiles.selectProfile}</p>
          <Input
            ref={profileSearchInputRef}
            name="profile_query"
            autoComplete="off"
            className="mt-2"
            value={profileQuery}
            onChange={(event) => setProfileQuery(event.target.value)}
            placeholder={messages.profiles.searchPlaceholder}
          />
          <p className="mt-1 truncate text-[11px] text-muted-foreground">{messages.profiles.activeShort(activeProfileName)}</p>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
          {hasNoProfiles ? (
            <div className="flex h-full items-center justify-center p-1">
                <div className="w-full rounded-lg border border-dashed bg-muted/30 px-4 py-6 text-center">
                  <p className="text-sm font-medium">{messages.profiles.noProfilesTitle}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {messages.profiles.noProfilesDescription}
                  </p>
                  <div className="mt-4 flex flex-col items-center justify-center gap-2 sm:flex-row">
                    <Button size="sm" className="h-8" onClick={onOpenCreateDialog} disabled={!canCreateProfile}>
                      {messages.profiles.createNewProfile}
                    </Button>
                    <Button size="sm" variant="link" className="h-8 px-2 text-xs" onClick={onManageProfiles}>
                      {messages.profiles.learnMore}
                    </Button>
                  </div>
                </div>
            </div>
          ) : hasNoMatches ? (
            <div className="flex h-full items-center justify-center p-1">
              <div className="w-full rounded-lg border border-dashed bg-muted/30 px-4 py-6 text-center">
                <p className="text-sm font-medium">{messages.profiles.noMatches}</p>
                <p className="mt-1 text-xs text-muted-foreground">{messages.profiles.tryDifferentSearchTerm}</p>
                <Button size="sm" variant="outline" className="mt-3 h-8" onClick={() => setProfileQuery("")}>
                  {messages.profiles.clearSearch}
                </Button>
              </div>
            </div>
          ) : (
            <div className="space-y-1">
              {filteredProfiles.map((profile) => {
                const isSelected = selectedProfileId === profile.id;
                const isActive = profile.is_active;

                return (
                  <button
                    key={profile.id}
                    type="button"
                    ref={isSelected ? selectedProfileButtonRef : null}
                    onClick={() => onSelectProfile(profile.id)}
                    className={cn(
                      "relative flex w-full items-start gap-3 rounded-md border border-transparent pl-3 pr-2 py-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/35",
                      isSelected ? "bg-accent/80" : "hover:bg-accent/55"
                    )}
                  >
                    {isSelected ? (
                      <span
                        aria-hidden="true"
                        className="absolute inset-y-1 left-0 w-0.5 rounded-r bg-primary"
                      />
                    ) : null}
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm font-semibold">{profile.name}</p>
                      <p className="mt-0.5 truncate text-xs text-muted-foreground">
                        {profile.description?.trim() || messages.profiles.noDescription}
                      </p>
                    </div>
                    <div className="ml-2 flex min-w-[132px] shrink-0 items-center justify-end gap-2">
                      {isActive ? (
                        <Badge
                          variant="outline"
                          className="h-5 shrink-0 border-emerald-500/40 bg-emerald-500/15 px-1.5 text-[10px] text-emerald-700 dark:text-emerald-200"
                        >
                          {messages.profiles.active}
                        </Badge>
                      ) : null}
                      {profile.is_default ? (
                        <Badge
                          variant="outline"
                          className="h-5 shrink-0 border-sky-500/40 bg-sky-500/10 px-1.5 text-[10px] text-sky-700 dark:text-sky-200"
                        >
                          {messages.profiles.default}
                        </Badge>
                      ) : null}
                      {!profile.is_editable ? (
                        <Badge
                          variant="outline"
                          className="h-5 shrink-0 border-amber-500/40 bg-amber-500/10 px-1.5 text-[10px] text-amber-700 dark:text-amber-200"
                        >
                          {messages.profiles.locked}
                        </Badge>
                      ) : null}
                      <Check
                        className={cn(
                          "h-4 w-4 shrink-0 text-primary transition-opacity",
                          isSelected ? "opacity-100" : "opacity-0"
                        )}
                      />
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </div>

        <div className="shrink-0 border-t">
          <div className="space-y-2 px-3 py-3">
            {!hasNoProfiles ? (
              <>
                <Button
                  variant="secondary"
                  className="h-8 w-full justify-start"
                  onClick={onOpenEditDialog}
                  disabled={Boolean(editDisabledReason)}
                  title={editDisabledReason ?? undefined}
                >
                  <Pencil className="mr-2 h-3.5 w-3.5" />
                  {messages.profiles.editSelected}
                </Button>
                <Button
                  variant="ghost"
                  className="h-8 w-full justify-start text-destructive/75 hover:text-destructive"
                  onClick={onOpenDeleteDialog}
                  disabled={Boolean(deleteDisabledReason)}
                  title={deleteDisabledReason ?? undefined}
                >
                  <Trash2 className="mr-2 h-3.5 w-3.5" />
                  {messages.profiles.deleteSelected}
                </Button>
                <div className="my-1 border-t" />
              </>
            ) : null}

            <Button variant="ghost" className="h-8 w-full justify-start" onClick={onManageProfiles}>
              {messages.profiles.manageProfiles}
            </Button>
            <Button
              variant="ghost"
              className="h-8 w-full justify-start"
              onClick={onOpenCreateDialog}
              disabled={!canCreateProfile}
            >
              {messages.profiles.createNewProfile}
            </Button>

            {!canCreateProfile ? (
              <p className="px-1 text-xs text-amber-700 dark:text-amber-200">
                {messages.profiles.limitReached}
              </p>
            ) : null}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
