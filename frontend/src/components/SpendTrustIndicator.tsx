import { Link } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { useLocale } from "@/i18n/useLocale";
import type { SpendTrustState } from "@/lib/costing";
import { cn } from "@/lib/utils";

const SPEND_TRUST_BADGE_CLASSES: Record<SpendTrustState, string> = {
  verified: "border-success/25 bg-success/10 text-success",
  fallback: "border-downgrade/35 bg-downgrade/15 text-amber-800 dark:text-amber-300",
  unpriced: "border-destructive/25 bg-destructive/10 text-destructive",
};

interface SpendTrustBadgeProps {
  spendTrust: SpendTrustState;
  className?: string;
}

interface SpendTrustNoteProps {
  spendTrust: SpendTrustState;
  className?: string;
  showPricingTemplatesLink?: boolean;
}

function getSpendTrustLabel(
  spendTrust: SpendTrustState,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  switch (spendTrust) {
    case "verified":
      return messages.spendTrust.verified;
    case "fallback":
      return messages.spendTrust.fallback;
    case "unpriced":
      return messages.spendTrust.unpriced;
  }
}

function getSpendTrustDescription(
  spendTrust: SpendTrustState,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  switch (spendTrust) {
    case "verified":
      return messages.spendTrust.verifiedDescription;
    case "fallback":
      return messages.spendTrust.fallbackDescription;
    case "unpriced":
      return messages.spendTrust.unpricedDescription;
  }
}

export function SpendTrustBadge({ spendTrust, className }: SpendTrustBadgeProps) {
  const { messages } = useLocale();

  return (
    <Badge
      variant="outline"
      className={cn(
        "rounded-full px-2 py-0.5 text-[10px] font-medium",
        SPEND_TRUST_BADGE_CLASSES[spendTrust],
        className,
      )}
    >
      {getSpendTrustLabel(spendTrust, messages)}
    </Badge>
  );
}

export function SpendTrustNote({
  spendTrust,
  className,
  showPricingTemplatesLink = false,
}: SpendTrustNoteProps) {
  const { messages } = useLocale();

  return (
    <div className={cn("flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground", className)}>
      <span>{getSpendTrustDescription(spendTrust, messages)}</span>
      {showPricingTemplatesLink ? (
        <Link
          className="font-medium text-primary underline-offset-4 hover:underline"
          to="/pricing-templates"
        >
          {messages.spendTrust.openPricingTemplates}
        </Link>
      ) : null}
    </div>
  );
}
