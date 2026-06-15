import { OperatorSwitchField } from "@/shared/design-system";

interface SwitchControllerProps {
  label: string;
  description?: string;
  checked?: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  className?: string;
}

/** @deprecated Use OperatorSwitchField from "@/shared/design-system" for new surfaces. */
export function SwitchController({
  label,
  description,
  checked,
  onCheckedChange,
  disabled,
  className,
}: SwitchControllerProps) {
  return (
    <OperatorSwitchField
      checked={checked}
      className={className}
      description={description}
      disabled={disabled}
      label={label}
      onCheckedChange={onCheckedChange}
    />
  );
}
