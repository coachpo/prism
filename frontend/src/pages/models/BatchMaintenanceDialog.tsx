import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldGroup, FieldLabel, FieldLegend, FieldSet } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { OperatorCallout, OperatorEmptyState, OperatorErrorState, OperatorLoadingState, OperatorSectionCard } from "@/shared/design-system";
import type { BatchMaintenanceAction } from "@/lib/types";
import { batchMaintenanceErrorMessage } from "@/i18n/batchMaintenance";
import { useBatchMaintenance } from "./useBatchMaintenance";

const actionLabels: Record<BatchMaintenanceAction, string> = {
  model_limits: "模型限额覆盖", model_strategy: "模型策略分配", target_pricing: "目标价格模板分配",
};
const fieldLabels: Record<string, string> = {
  context_limit: "上下文上限", output_limit: "输出上限", loadbalance_strategy_id: "路由策略编号",
  pricing_template_name: "价格模板名称", revision_id: "价格模板版本", currency: "币种",
  pricing_template_id: "价格模板编号", reference_id: "引用编号", client: "客户端",
  override: "人工覆盖", has_override: "已有人工覆盖", model_id: "模型标识", name: "名称",
};
function displayValue(value: unknown): string {
  if (value === null || value === undefined) return "—";
  if (typeof value === "boolean") return value ? "是" : "否";
  if (typeof value === "object") return "复合配置（由服务端校验）";
  return String(value);
}
export function BatchMaintenanceDialog({ open, onOpenChange, onApplied }: {
  open: boolean; onOpenChange: (open: boolean) => void; onApplied: () => void | Promise<void>;
}) {
  const s = useBatchMaintenance(open, onApplied);
  const options = s.action === "target_pricing"
    ? s.choices?.targets.map((target) => ({ id: target.id, label: `${target.name ?? "未命名目标"} · #${target.id}` }))
    : s.choices?.models.map((model) => ({ id: model.id, label: model.display_name ? `${model.display_name} · ${model.model_id}` : model.model_id }));
  const references = s.action === "model_strategy" ? s.choices?.strategies : s.choices?.prices;
  const manual = s.preview?.items.some((item) => item.manual_override);
  return <Dialog open={open} onOpenChange={(value) => { if (!s.applying) onOpenChange(value); }}>
    <DialogContent size="lg" className="flex max-h-[90dvh] flex-col">
      <DialogHeader><DialogTitle>批量维护</DialogTitle><DialogDescription>每批最多 20 项。逐项预览后一次应用；任何失效或非法项均阻止整批写入。</DialogDescription></DialogHeader>
      <div className="flex min-h-0 flex-col gap-4 overflow-y-auto pr-1">
        <FieldGroup>
          <Field><FieldLabel>维护动作</FieldLabel>
            <ToggleGroup type="single" value={s.action} disabled={s.busy} className="flex-wrap" aria-label="维护动作" onValueChange={(value) => { if (value) s.changeAction(value as BatchMaintenanceAction); }}>
              {(Object.entries(actionLabels) as Array<[BatchMaintenanceAction, string]>).map(([action, label]) => <ToggleGroupItem key={action} value={action}>{label}</ToggleGroupItem>)}
            </ToggleGroup>
          </Field>
          {s.action === "model_limits" && <OperatorCallout description="填写已核实的 models.dev / OpenCode 限额；不会推测缺失值。Pi 的独立绑定和限额不受此动作影响。" />}
          {s.busy && !s.choices && <OperatorLoadingState title="正在读取模型和引用" />}
          {s.error && <OperatorErrorState title="批量维护未完成" description={s.error} action={!s.choices ? <Button variant="outline" onClick={s.retry}>重新读取</Button> : undefined} />}
          {s.success && <OperatorCallout intent="success" title="整批已应用" description="已完成服务端权威回读，相关配置已刷新。" />}
          {s.choices && <>
            {s.action !== "model_limits" && <Field><FieldLabel htmlFor="batch-reference">{s.action === "model_strategy" ? "分配路由策略" : "分配价格模板"}</FieldLabel>
              <Select value={s.reference} onValueChange={s.changeReference} disabled={s.busy}><SelectTrigger id="batch-reference"><SelectValue placeholder="请选择引用" /></SelectTrigger>
                <SelectContent><SelectGroup>{references?.map((reference) => <SelectItem key={reference.id} value={String(reference.id)}>{reference.name}</SelectItem>)}</SelectGroup></SelectContent>
              </Select>
            </Field>}
            <FieldSet disabled={s.busy}><FieldLegend>明确选择{s.action === "target_pricing" ? "终端目标" : "模型"} · 已选 {s.selected.length} / 20</FieldLegend>
              {!options?.length && <OperatorEmptyState title="没有可选择的配置" description="请先创建相应模型或终端目标。" />}
              <FieldGroup>{options?.map((item) => <Field key={item.id} orientation="horizontal">
                <Checkbox id={`batch-item-${item.id}`} checked={s.selected.includes(item.id)} disabled={s.busy || (!s.selected.includes(item.id) && s.selected.length >= 20)} onCheckedChange={() => s.toggle(item.id)} />
                <FieldLabel htmlFor={`batch-item-${item.id}`}>{item.label}</FieldLabel>
              </Field>)}</FieldGroup>
            </FieldSet>
            {s.action === "model_limits" && s.selected.map((id) => <OperatorSectionCard key={id} title={options?.find((item) => item.id === id)?.label ?? `模型 #${id}`}>
              <FieldGroup>{(["context", "output"] as const).map((key) => <Field key={key}>
                <FieldLabel htmlFor={`batch-${key}-${id}`}>{key === "context" ? "上下文上限" : "输出上限"}</FieldLabel>
                <Input id={`batch-${key}-${id}`} inputMode="numeric" value={s.limits[id]?.[key] ?? ""} disabled={s.busy} onChange={(event) => s.changeLimit(id, key, event.target.value)} />
              </Field>)}</FieldGroup>
            </OperatorSectionCard>)}
          </>}
        </FieldGroup>
        {s.preview && <section aria-label="逐项差异" className="flex flex-col gap-3"><h2>逐项差异</h2>
          {s.preview.items.map((item) => <OperatorSectionCard key={item.id} title={item.label} description={[item.manual_override ? "此项已有人工覆盖，需要明确确认" : "", s.action !== "model_limits" ? `所选引用：${references?.find((reference) => String(reference.id) === s.reference)?.name ?? "引用名称未知"}` : ""].filter(Boolean).join("；") || undefined}>
            {item.error && <OperatorCallout intent="danger" description={batchMaintenanceErrorMessage(item.error)} />}
            <dl className="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-2">{["context_limit", "output_limit", "loadbalance_strategy_id", "pricing_template_id"].filter((key) => Object.hasOwn(item.after, key)).map((key) => <div key={key} className="contents">
              <dt>{fieldLabels[key] ?? "其他配置"}</dt><dd className="break-all font-mono">{displayValue(item.before[key])} → {displayValue(item.after[key])}</dd>
            </div>)}</dl>
          </OperatorSectionCard>)}
          {manual && <Field orientation="horizontal"><Checkbox id="batch-confirm-manual" checked={s.confirmed} onCheckedChange={(value) => s.setConfirmed(value === true)} disabled={s.busy} /><FieldLabel htmlFor="batch-confirm-manual">确认按上方差异替换已有人工覆盖</FieldLabel></Field>}
          {!s.preview.can_apply && <OperatorCallout intent="warning" description="当前预览不能应用。修正标记的问题后重新预览。" />}
        </section>}
      </div>
      <DialogFooter>
        <Button variant="outline" disabled={s.applying} onClick={() => onOpenChange(false)}>关闭</Button>
        <Button variant="outline" disabled={s.busy || !s.choices || !s.selected.length || (s.action !== "model_limits" && !s.reference)} onClick={() => void s.run(false)}>{s.busy ? "处理中…" : "预览差异"}</Button>
        <Button disabled={s.busy || !s.preview?.can_apply || (manual && !s.confirmed)} onClick={() => void s.run(true)}>原子应用</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}
