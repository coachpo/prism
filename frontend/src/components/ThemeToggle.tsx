import { useTheme } from "next-themes";
import { Check, Laptop, Moon, Sun } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";

type ThemeOption = {
  value: "light" | "dark" | "system";
  label: string;
  icon: typeof Sun;
};

type ThemeToggleProps = {
  align?: "start" | "center" | "end";
  buttonClassName?: string;
  menuClassName?: string;
};

type ThemeMenuItemsProps = {
  theme: string;
  themeOptions: readonly ThemeOption[];
  setTheme: (value: ThemeOption["value"]) => void;
};

export function ThemeMenuItems({ theme, themeOptions, setTheme }: ThemeMenuItemsProps) {
  return themeOptions.map((option) => {
    const OptionIcon = option.icon;

    return (
      <DropdownMenuItem
        key={option.value}
        onClick={() => setTheme(option.value)}
        className="justify-between"
      >
        <span className="inline-flex items-center gap-2">
          <OptionIcon className="size-4 text-muted-foreground" />
          {option.label}
        </span>
        <Check
          className={cn(
            "size-4 text-primary transition-opacity",
            theme === option.value ? "opacity-100" : "opacity-0",
          )}
        />
      </DropdownMenuItem>
    );
  });
}

export function ThemeToggle({
  align = "end",
  buttonClassName,
  menuClassName,
}: ThemeToggleProps) {
  const { messages } = useLocale();
  const { theme = "system", setTheme } = useTheme();
  const themeOptions = [
    { value: "light", label: messages.theme.light, icon: Sun },
    { value: "dark", label: messages.theme.dark, icon: Moon },
    { value: "system", label: messages.theme.system, icon: Laptop },
  ] as const;
  const activeTheme = themeOptions.find((option) => option.value === theme) ?? themeOptions[2];
  const ActiveIcon = activeTheme.icon;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className={cn(
            "rounded-md text-muted-foreground transition-colors hover:bg-surface-container-low hover:text-foreground dark:hover:bg-accent/60",
            buttonClassName,
          )}
        >
          <ActiveIcon />
          <span className="sr-only">{messages.theme.changeTheme}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className={cn("w-36", menuClassName)}>
        <ThemeMenuItems theme={theme} themeOptions={themeOptions} setTheme={setTheme} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
