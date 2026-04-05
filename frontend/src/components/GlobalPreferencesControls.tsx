import { LanguageSwitcher } from "@/components/LanguageSwitcher";
import { ThemeToggle } from "@/components/ThemeToggle";
import { cn } from "@/lib/utils";

type GlobalPreferencesControlsProps = {
  className?: string;
  languageSwitcherClassName?: string;
  themeToggleButtonClassName?: string;
  themeToggleMenuClassName?: string;
};

export function GlobalPreferencesControls({
  className,
  languageSwitcherClassName,
  themeToggleButtonClassName,
  themeToggleMenuClassName,
}: GlobalPreferencesControlsProps) {
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <LanguageSwitcher buttonClassName={languageSwitcherClassName} />
      <ThemeToggle
        buttonClassName={themeToggleButtonClassName}
        menuClassName={themeToggleMenuClassName}
      />
    </div>
  );
}
