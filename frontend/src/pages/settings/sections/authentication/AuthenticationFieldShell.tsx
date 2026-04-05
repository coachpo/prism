import type { ReactNode } from "react";
import { Label } from "@/components/ui/label";

interface AuthenticationFieldShellProps {
  label: string;
  helper?: string;
  helperClassName?: string;
  htmlFor?: string;
  children: ReactNode;
}

export function AuthenticationFieldShell({
  label,
  helper,
  helperClassName,
  htmlFor,
  children,
}: AuthenticationFieldShellProps) {
  return (
    <div className="space-y-2">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
      {helper ? (
        <p className={`text-xs ${helperClassName ?? "text-muted-foreground"}`}>
          {helper}
        </p>
      ) : null}
    </div>
  );
}
