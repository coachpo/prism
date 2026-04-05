import { cn } from "@/lib/utils";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getVendorIconPreset } from "@/components/vendorIconRegistry";

type VendorIconLike = {
  key?: string | null;
  name?: string | null;
  icon_key?: string | null;
};

interface VendorIconProps {
  vendor?: VendorIconLike | null;
  size?: number;
  className?: string;
  decorative?: boolean;
}

function getVendorLabel(vendor?: VendorIconLike | null) {
  const name = vendor?.name?.trim();
  if (name) {
    return name;
  }

  const key = vendor?.key?.trim();
  return key || null;
}

function getVendorMonogram(vendor?: VendorIconLike | null) {
  const label = getVendorLabel(vendor);
  const match = label?.match(/[A-Za-z0-9]/u);
  return match ? match[0].toUpperCase() : null;
}

export function VendorIcon({ vendor, size = 16, className, decorative = false }: VendorIconProps) {
  const messages = getStaticMessages();
  const preset = getVendorIconPreset(vendor?.icon_key);
  const label = getVendorLabel(vendor);
  const ariaLabel = label ? messages.common.vendorIconLabel(label) : messages.common.vendorIconPlaceholder;
  const monogram = getVendorMonogram(vendor) ?? "?";
  const accessibilityProps = decorative ? { "aria-hidden": true as const } : { role: "img" as const, "aria-label": ariaLabel };

  return (
    <span
      {...accessibilityProps}
      className={cn(
        "inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md border border-border/60 bg-muted/35 text-foreground",
        className,
      )}
      style={{ width: size, height: size, fontSize: Math.max(10, Math.round(size * 0.55)) }}
    >
      {preset ? (
        <preset.Icon className="h-full w-full" aria-hidden="true" />
      ) : (
        <span className="font-medium leading-none">{monogram}</span>
      )}
    </span>
  );
}
