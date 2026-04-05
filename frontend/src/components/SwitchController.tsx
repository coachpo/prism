import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

interface SwitchControllerProps {
  label: string;
  description?: string;
  checked?: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  className?: string;
}

export function SwitchController({
  label,
  description,
  checked,
  onCheckedChange,
  disabled,
  className,
}: SwitchControllerProps) {
  return (
    <div className={cn("flex items-center justify-between rounded-lg border p-3", className)}>
      <div>
        <Label>{label}</Label>
        {description && (
          <p className="text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <Switch
        checked={checked ?? false}
        onCheckedChange={onCheckedChange}
        disabled={disabled}
        className="data-[state=checked]:bg-emerald-500"
      />
    </div>
  );
}
