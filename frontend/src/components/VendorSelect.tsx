import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { VendorIcon } from "@/components/VendorIcon";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { Vendor } from "@/lib/types";

interface VendorSelectProps {
  value: string;
  onValueChange: (value: string) => void;
  valueType?: "vendor_id" | "vendor_key";
  showAll?: boolean;
  allLabel?: string;
  vendors: Vendor[];
  className?: string;
  placeholder?: string;
}

export function VendorSelect({
  value,
  onValueChange,
  valueType = "vendor_id",
  showAll = true,
  allLabel,
  vendors,
  className,
  placeholder,
}: VendorSelectProps) {
  const messages = getStaticMessages();
  const resolvedAllLabel = allLabel ?? `${messages.statistics.all} ${messages.common.vendor}`;
  const resolvedPlaceholder = placeholder ?? messages.common.vendor;
  const itemValue = (vendor: Vendor) =>
    valueType === "vendor_id" ? String(vendor.id) : vendor.key;
  const selectedVendor = vendors.find((vendor) => itemValue(vendor) === value);
  const isAllSelected = showAll && value === "all";
  const selectedContent = selectedVendor ? (
    <span className="flex min-w-0 max-w-full items-center gap-2">
      <VendorIcon vendor={selectedVendor} size={16} />
      <span className="truncate">{selectedVendor.name}</span>
    </span>
  ) : isAllSelected ? (
    <span>{resolvedAllLabel}</span>
  ) : null;

  return (
    <Select value={value} onValueChange={onValueChange}>
      <SelectTrigger className={className}>
        <SelectValue placeholder={resolvedPlaceholder}>{selectedContent}</SelectValue>
      </SelectTrigger>
      <SelectContent>
        {showAll ? <SelectItem value="all">{resolvedAllLabel}</SelectItem> : null}
        {vendors.map((vendor) => (
          <SelectItem key={vendor.id} value={itemValue(vendor)}>
            <span className="flex items-center gap-2">
              <VendorIcon vendor={vendor} size={14} />
              <span>{vendor.name}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
