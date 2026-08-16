import type { ReactNode } from "react";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";

interface AuthenticationFieldShellProps {
  label: string;
  helper?: string;
  helperClassName?: string;
  htmlFor?: string;
  descriptionId?: string;
  children: ReactNode;
}

export function AuthenticationFieldShell({
  label,
  helper,
  helperClassName,
  htmlFor,
  descriptionId,
  children,
}: AuthenticationFieldShellProps) {
  return (
    <Field>
      <FieldLabel htmlFor={htmlFor}>{label}</FieldLabel>
      {children}
      {helper ? (
        <FieldDescription id={descriptionId} className={helperClassName}>
          {helper}
        </FieldDescription>
      ) : null}
    </Field>
  );
}
