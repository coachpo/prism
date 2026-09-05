import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { formatApiFamily } from "@/components/apiFamilyPresentation";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { ApiFamily } from "@/lib/types";

interface ApiFamilySelectProps {
  value: string;
  onValueChange: (value: string) => void;
  apiFamilies?: ApiFamily[];
  showAll?: boolean;
  allLabel?: string;
  className?: string;
  placeholder?: string;
  /** 让调用方的 <Label htmlFor> 能关联到触发器，否则读屏只会听到一个无名下拉。 */
  id?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
}

const DEFAULT_API_FAMILIES: ApiFamily[] = ["openai", "anthropic", "gemini"];

export function ApiFamilySelect({
  value,
  onValueChange,
  apiFamilies = DEFAULT_API_FAMILIES,
  showAll = true,
  allLabel,
  className,
  placeholder,
  id,
  "aria-label": ariaLabel,
  "aria-labelledby": ariaLabelledBy,
}: ApiFamilySelectProps) {
  const messages = getStaticMessages();
  const resolvedAllLabel = allLabel ?? `${messages.statistics.all} ${messages.common.apiFamily}`;
  const resolvedPlaceholder = placeholder ?? messages.common.apiFamily;
  return (
    <Select value={value} onValueChange={onValueChange}>
      <SelectTrigger
        id={id}
        aria-label={ariaLabel}
        aria-labelledby={ariaLabelledBy}
        className={className}
      >
        <SelectValue placeholder={resolvedPlaceholder} />
      </SelectTrigger>
      <SelectContent>
        {showAll ? <SelectItem value="all">{resolvedAllLabel}</SelectItem> : null}
        {apiFamilies.map((apiFamily) => (
          <SelectItem key={apiFamily} value={apiFamily}>
            <span className="flex items-center gap-2">
              <ApiFamilyIcon apiFamily={apiFamily} size={14} />
              {formatApiFamily(apiFamily)}
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
