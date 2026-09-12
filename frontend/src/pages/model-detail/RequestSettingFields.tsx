import { useId } from "react";
import { Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { FieldError } from "@/components/ui/field";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useLocale } from "@/i18n/useLocale";
import { emptySetting, numericSettingValue, settingEntry, type SettingKind, type SettingNode } from "./structuredRequestSettings";

interface Props { node: SettingNode; onChange: (node: SettingNode) => void; label: string }

export function RequestSettingFields({ node, onChange, label }: Props) {
  const { messages } = useLocale();
  const copy = messages.modelDetail;
  const id = useId();
  if (node.kind === "group" || node.kind === "list") {
    return <div className="flex min-w-0 flex-col gap-3" role="group" aria-label={label}>
      {node.entries.map((entry, index) => <div key={entry.id} className="flex min-w-0 flex-col gap-2 rounded-md border border-border p-3">
        <div className="flex min-w-0 items-center gap-2">
          {node.kind === "group" ? <div className="min-w-0 flex-1">
            <Label className="sr-only" htmlFor={`${id}-${entry.id}`}>{copy.settingName}</Label>
            <Input id={`${id}-${entry.id}`} value={entry.name} aria-invalid={!entry.name.trim()} placeholder={copy.settingName} className="font-mono" onChange={(event) => onChange({ ...node, entries: node.entries.map((item) => item.id === entry.id ? { ...item, name: event.target.value } : item) })} />
          </div> : <span className="flex-1 text-sm">{copy.settingListItem(index + 1)}</span>}
          <span className="text-xs text-muted-foreground">{copy.settingKinds[entry.node.kind]}</span>
          <Tooltip><TooltipTrigger asChild><Button type="button" size="icon-xs" variant="ghost" aria-label={copy.removeSetting(entry.name || copy.settingListItem(index + 1))} onClick={() => onChange({ ...node, entries: node.entries.filter((item) => item.id !== entry.id) })}><X /></Button></TooltipTrigger><TooltipContent>{copy.removeSetting(entry.name || copy.settingListItem(index + 1))}</TooltipContent></Tooltip>
        </div>
        <RequestSettingFields node={entry.node} label={`${label} · ${entry.name || copy.settingListItem(index + 1)}`} onChange={(next) => onChange({ ...node, entries: node.entries.map((item) => item.id === entry.id ? { ...item, node: next } : item) })} />
      </div>)}
      <DropdownMenu>
        <DropdownMenuTrigger asChild><Button type="button" variant="outline" size="sm" className="self-start"><Plus data-icon="inline-start" />{node.kind === "group" ? copy.addSetting : copy.addListItem}</Button></DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuGroup>
          {(Object.keys(copy.settingKinds) as SettingKind[]).map((kind) => <DropdownMenuItem key={kind} onSelect={() => onChange({ ...node, entries: [...node.entries, settingEntry("", emptySetting(kind))] })}>{copy.settingKinds[kind]}</DropdownMenuItem>)}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>;
  }
  if (node.kind === "empty") return <p className="text-xs text-muted-foreground">{copy.settingEmptyValue}</p>;
  if (node.kind === "toggle") return <div className="flex items-center gap-2"><Switch id={id} checked={node.value} aria-label={label} onCheckedChange={(value) => onChange({ ...node, value })} /><Label htmlFor={id}>{node.value ? copy.settingOn : copy.settingOff}</Label></div>;
  if (node.kind === "text") return <div className="flex min-w-0 flex-col gap-1.5"><Label htmlFor={id}>{copy.settingValue}</Label><Textarea id={id} aria-label={label} rows={2} value={node.value} onChange={(event) => onChange({ ...node, value: event.target.value })} /></div>;
  const invalidNumber = numericSettingValue(node.value) === null;
  return <div className="flex min-w-0 flex-col gap-1.5"><Label htmlFor={id}>{copy.settingValue}</Label><Input id={id} aria-label={label} aria-invalid={invalidNumber} aria-describedby={invalidNumber ? `${id}-error` : undefined} inputMode="decimal" value={node.value} onChange={(event) => onChange({ ...node, value: event.target.value })} />{invalidNumber ? <FieldError id={`${id}-error`}>{copy.settingNumberInvalid}</FieldError> : null}</div>;
}
