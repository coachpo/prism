import { useTheme } from "next-themes";
import { ChevronsUpDown, Languages, Laptop, LogOut, Moon, MoonStar, Sun } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuPortal,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarMenu, SidebarMenuButton, SidebarMenuItem } from "@/components/ui/sidebar";
import { useSidebar } from "@/components/ui/sidebar-context";
import { LanguageMenuItems } from "@/components/LanguageSwitcher";
import { ThemeMenuItems } from "@/components/ThemeToggle";
import { useLocale } from "@/i18n/useLocale";
import { VERSION_LABEL } from "./navigationProfileConfig";

type Props = {
  authEnabled: boolean;
  handleLogout: () => Promise<void>;
  username: string | null;
};

export function NavUser({ authEnabled, handleLogout, username }: Props) {
  const { locale, messages, setLocale } = useLocale();
  const { theme = "system", setTheme } = useTheme();
  const { isMobile } = useSidebar();

  const displayName = authEnabled
    ? username?.trim() || messages.shell.signedOut
    : messages.settingsAuthentication.authenticationDisabled;
  const languageOptions = [
    { value: "en", label: messages.locale.options.en },
    { value: "zh-CN", label: messages.locale.options["zh-CN"] },
  ] as const;
  const themeOptions = [
    { value: "light", label: messages.theme.light, icon: Sun },
    { value: "dark", label: messages.theme.dark, icon: Moon },
    { value: "system", label: messages.theme.system, icon: Laptop },
  ] as const;

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              tooltip={`${displayName} · ${VERSION_LABEL}`}
              className="rounded-xl border border-sidebar-border/70 bg-sidebar-accent/35 px-2.5 py-2 shadow-sm hover:bg-sidebar-accent/60 data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <div className="grid min-w-0 flex-1 text-left leading-tight group-data-[collapsible=icon]:hidden">
                <span className="truncate text-sm font-semibold">{displayName}</span>
                <span className="truncate text-[11px] text-sidebar-foreground/60">{VERSION_LABEL}</span>
              </div>
              <ChevronsUpDown className="ml-auto text-sidebar-foreground/60 group-data-[collapsible=icon]:hidden" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side={isMobile ? "bottom" : "right"} className="min-w-64">
            <DropdownMenuLabel className="grid gap-0.5">
              <span className="truncate text-sm font-semibold">{displayName}</span>
              <span className="truncate text-xs font-normal text-muted-foreground">{VERSION_LABEL}</span>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <Languages />
                  {messages.locale.changeLanguage}
                </DropdownMenuSubTrigger>
                <DropdownMenuPortal>
                  <DropdownMenuSubContent>
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>{messages.locale.changeLanguage}</DropdownMenuLabel>
                      <LanguageMenuItems locale={locale} languageOptions={languageOptions} setLocale={setLocale} />
                    </DropdownMenuGroup>
                  </DropdownMenuSubContent>
                </DropdownMenuPortal>
              </DropdownMenuSub>
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <MoonStar />
                  {messages.theme.changeTheme}
                </DropdownMenuSubTrigger>
                <DropdownMenuPortal>
                  <DropdownMenuSubContent>
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>{messages.theme.changeTheme}</DropdownMenuLabel>
                      <ThemeMenuItems theme={theme} themeOptions={themeOptions} setTheme={setTheme} />
                    </DropdownMenuGroup>
                  </DropdownMenuSubContent>
                </DropdownMenuPortal>
              </DropdownMenuSub>
            </DropdownMenuGroup>
            {authEnabled ? <DropdownMenuSeparator /> : null}
            {authEnabled ? (
              <DropdownMenuGroup>
                <DropdownMenuItem variant="destructive" onSelect={() => void handleLogout()}>
                  <LogOut />
                  {messages.shell.signOut}
                </DropdownMenuItem>
              </DropdownMenuGroup>
            ) : null}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
