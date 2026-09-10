import { useCallback, useEffect, useRef, useState } from "react";
import { batchMaintenanceErrorMessage } from "@/i18n/batchMaintenance";
import { api } from "@/lib/api";
import { clearSharedReferenceData, getSharedConnectionOptions, getSharedLoadbalanceStrategies, getSharedModels, getSharedPricingTemplates } from "@/lib/referenceData";
import type { BatchMaintenanceAction, BatchMaintenanceRequest, BatchMaintenanceResponse } from "@/lib/types";

async function loadChoices() {
  const [models, targets, strategies, prices] = await Promise.all([
    getSharedModels(0, true), getSharedConnectionOptions(0, true),
    getSharedLoadbalanceStrategies(0, true), getSharedPricingTemplates(0, true),
  ]);
  return { models, targets, strategies, prices };
}
export function useBatchMaintenance(open: boolean, onApplied: () => void | Promise<void>) {
  const [choices, setChoices] = useState<Awaited<ReturnType<typeof loadChoices>> | null>(null);
  const [action, setAction] = useState<BatchMaintenanceAction>("model_limits");
  const [selected, setSelected] = useState<number[]>([]);
  const [limits, setLimits] = useState<Record<number, { context: string; output: string }>>({});
  const [reference, setReference] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [preview, setPreview] = useState<BatchMaintenanceResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [applying, setApplying] = useState(false);
  const [success, setSuccess] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => {
    const session = ++generation.current;
    if (open) {
      setBusy(true); setChoices(null); setError(null); setPreview(null); setSuccess(false);
      setSelected([]); setLimits({}); setReference(""); setConfirmed(false);
      void loadChoices().then((data) => { if (generation.current === session) setChoices(data); })
        .catch((cause: unknown) => { if (generation.current === session) setError(batchMaintenanceErrorMessage(cause)); })
        .finally(() => { if (generation.current === session) setBusy(false); });
    }
    return () => { generation.current = session + 1; controller.current?.abort(); };
  }, [open, attempt]);
  const invalidate = () => { setPreview(null); setConfirmed(false); setSuccess(false); setError(null); };
  const changeAction = (value: BatchMaintenanceAction) => { invalidate(); setAction(value); setSelected([]); setReference(""); };
  const toggle = (id: number) => {
    invalidate();
    setSelected((ids) => ids.includes(id) ? ids.filter((entry) => entry !== id) : ids.length < 20 ? [...ids, id] : ids);
  };
  const changeLimit = (id: number, key: "context" | "output", value: string) => {
    invalidate(); setLimits((current) => ({ ...current, [id]: { context: current[id]?.context ?? "", output: current[id]?.output ?? "", [key]: value } }));
  };
  const request = (): BatchMaintenanceRequest => {
    if (!selected.length || selected.length > 20) throw new Error("请选择 1–20 项。");
    if (action !== "model_limits" && (!Number.isSafeInteger(Number(reference)) || Number(reference) <= 0)) {
      throw new Error("请选择仍有效的路由策略或价格模板。");
    }
    return {
      items: selected.map((id) => {
        if (action !== "model_limits") return { id };
        const context = Number(limits[id]?.context), output = Number(limits[id]?.output);
        if (!Number.isSafeInteger(context) || !Number.isSafeInteger(output) || context <= 0 || output <= 0 || output > context) {
          throw new Error("请逐项填写可靠的正整数限额，输出上限不能超过上下文上限。");
        }
        return { id, context_limit: context, output_limit: output };
      }),
      ...(action === "model_limits" ? { client: "opencode" as const } : { reference_id: Number(reference) }),
      confirm_manual_overrides: confirmed,
    };
  };
  const run = async (apply: boolean) => {
    const session = generation.current;
    let data: BatchMaintenanceRequest;
    try { data = request(); } catch (cause) { setError((cause as Error).message); return; }
    setBusy(true); setApplying(apply); setError(null); setSuccess(false);
    if (!apply) setConfirmed(false);
    const abort = new AbortController(); controller.current = abort;
    let applied = false;
    try {
      const result = apply
        ? await api.batchMaintenance.apply(action, { ...data, preview_token: preview?.preview_token }, abort.signal)
        : await api.batchMaintenance.preview(action, data, abort.signal);
      if (session !== generation.current) return;
      setPreview(result);
      if (apply && result.applied) {
        applied = true;
        clearSharedReferenceData("models"); clearSharedReferenceData("connections");
        const authoritative = await loadChoices();
        if (session !== generation.current) return;
        setChoices(authoritative);
        await onApplied();
        if (session !== generation.current) return;
        setSuccess(true); setSelected([]); setPreview(null); setConfirmed(false);
      }
    } catch (cause) {
      if (session !== generation.current) return;
      setPreview(null); setConfirmed(false);
      setError(applied ? "整批已应用，但权威回读失败。请刷新相关页面确认当前配置。" : batchMaintenanceErrorMessage(cause));
    } finally { if (session === generation.current) { setBusy(false); setApplying(false); } }
  };
  return { choices, action, selected, limits, reference, confirmed, preview, error, busy, applying, success,
    changeAction, toggle, changeLimit, changeReference: (value: string) => { invalidate(); setReference(value); },
    setConfirmed, run, retry: useCallback(() => setAttempt((value) => value + 1), []),
  };
}
