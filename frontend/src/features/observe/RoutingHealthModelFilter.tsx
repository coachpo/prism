import { useRef, useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { getSharedModels } from "@/lib/referenceData"
import type { ModelConfigListItem } from "@/lib/types"
import { useLocale } from "@/i18n/useLocale"

export function RoutingHealthModelFilter({ id, value, onValueChange }: { id?: string; value?: string; onValueChange: (value: string | undefined) => void }) {
  const { messages } = useLocale()
  const copy = messages.routingHealth
  const [models, setModels] = useState<ModelConfigListItem[]>([])
  const [phase, setPhase] = useState<"idle" | "loading" | "ready" | "error">("idle")
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const load = async () => {
    setPhase("loading")
    try {
      const rows = await getSharedModels(0, true)
      if (mounted.current) { setModels(rows); setPhase("ready") }
    } catch {
      if (mounted.current) setPhase("error")
    }
  }
  return (
    <div className="flex min-w-44 flex-col gap-1">
      <Select value={value ? `model:${value}` : "all"} onValueChange={next => onValueChange(next === "all" ? undefined : next.slice(6))} onOpenChange={open => { if (open && phase !== "loading") void load() }}>
        <SelectTrigger id={id} className="w-full" aria-label={copy.modelFilterLabel}><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value="all">{copy.allModels}</SelectItem>
          {value && !models.some(model => model.model_id === value) && <SelectItem value={`model:${value}`}>{value}</SelectItem>}
          {models.map(model => <SelectItem key={model.id} value={`model:${model.model_id}`}>{model.display_name ? `${model.display_name} · ${model.model_id}` : model.model_id}</SelectItem>)}
          {phase === "loading" && <SelectItem value="__loading" disabled>{copy.modelsLoading}</SelectItem>}
        </SelectGroup></SelectContent>
      </Select>
      {phase === "error" && <p className="text-xs text-muted-foreground" role="status">{copy.modelsLoadFailed} <Button variant="link" size="sm" onClick={() => void load()}>{copy.retry}</Button></p>}
    </div>
  )
}
