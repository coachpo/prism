import { ChevronDown, LogOut, UserRound } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useLocale } from "@/i18n/useLocale";

type Props = {
  authEnabled: boolean;
  handleLogout: () => Promise<void>;
  username: string | null;
};

export function HeaderAccountMenu({ authEnabled, handleLogout, username }: Props) {
  const { messages } = useLocale();
  const copy = messages.shell;
  const displayName = username?.trim() || copy.signedOut;

  if (!authEnabled) {
    return null;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          data-testid="header-account"
          className="h-[var(--density-control-h-sm)] gap-1.5 rounded-md px-2 text-muted-foreground hover:text-foreground"
        >
          <UserRound aria-hidden="true" className="size-4" />
          <span className="max-w-32 truncate text-xs">{displayName}</span>
          <ChevronDown aria-hidden="true" className="size-3.5" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-52">
        <DropdownMenuLabel className="grid gap-0.5">
          <span className="text-xs font-normal text-muted-foreground">{copy.account}</span>
          <span className="truncate text-sm font-semibold">{displayName}</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => void handleLogout()}>
          <LogOut aria-hidden="true" />
          {copy.signOut}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
