import { Check, Languages } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";

type LanguageSwitcherProps = {
  align?: "start" | "center" | "end";
  buttonClassName?: string;
  menuClassName?: string;
};

export function LanguageSwitcher({
  align = "end",
  buttonClassName,
  menuClassName,
}: LanguageSwitcherProps) {
  const { locale, messages, setLocale } = useLocale();
  const languageOptions = [
    { value: "en", label: messages.locale.options.en },
    { value: "zh-CN", label: messages.locale.options["zh-CN"] },
  ] as const;
  const activeLanguage =
    languageOptions.find((option) => option.value === locale) ?? languageOptions[0];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className={cn(
            "h-8 gap-1.5 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground dark:hover:bg-accent/60",
            buttonClassName,
          )}
        >
          <Languages className="h-4 w-4" />
          <span className="max-w-[5.5rem] truncate">{activeLanguage.label}</span>
          <span className="sr-only">{messages.locale.changeLanguage}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className={cn("w-36", menuClassName)}>
        {languageOptions.map((option) => (
          <DropdownMenuItem
            key={option.value}
            onClick={() => setLocale(option.value)}
            className="justify-between"
          >
            {option.label}
            <Check
              className={cn(
                "h-4 w-4 text-primary transition-opacity",
                locale === option.value ? "opacity-100" : "opacity-0",
              )}
            />
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
