import { useEffect, useRef, useState } from 'react';
import { authSessionCoordinator } from '@/context/auth/coordinatorInstance';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Field, FieldLabel } from '@/components/ui/field';
import { OperatorCallout } from '@/shared/design-system';
import { api } from '@/lib/api';
import { useLocale } from '@/i18n/useLocale';
import { getSharedModels, getSharedEndpoints, getSharedConnectionOptions, getSharedProxyKeys } from '@/lib/referenceData';
import { downloadPreference, PreferenceFileTooLargeError, readPreferenceFile } from '@/lib/preferences/storage';
import { applySavedView, deleteRequestLogView, importSavedViews, loadSavedViewsResult, saveRequestLogView, type SavedRequestLogView } from './requestLogSavedViews';
import { invalidViewReferences, parseRequestViewsFile, requestViewsFile, VIEW_REFERENCE_KEYS, type ViewReferenceKey, type ViewReferenceOptions } from './requestViewTransfer';
import type { RequestLogPageActions } from './useRequestLogPageState';
import type { RequestLogFilterOptions } from './requestLogQuery';
interface Props { actions: RequestLogPageActions; filterOptions: RequestLogFilterOptions; filterOptionsLoaded: boolean; costSegments?: { value: string; label: string }[] }
export function RequestViewsPanel({ actions, filterOptions, filterOptionsLoaded, costSegments = [] }: Props) {
  const { messages } = useLocale(); const copy = messages.preferences;
  const fileInput = useRef<HTMLInputElement>(null);
  const [stored, setStored] = useState(loadSavedViewsResult);
  const [open, setOpen] = useState(false); const [name, setName] = useState(''); const [notice, setNotice] = useState('');
  const [pending, setPending] = useState<SavedRequestLogView[]>([]);
  const [editing, setEditing] = useState<SavedRequestLogView | null>(null);
  const [options, setOptions] = useState<ViewReferenceOptions | null>(null);
  const [busy, setBusy] = useState(false);
  const lifetime = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController(); lifetime.current = controller;
    const epoch = authSessionCoordinator.getEpoch();
    const unsubscribe = authSessionCoordinator.subscribe(() => { if (authSessionCoordinator.getEpoch() !== epoch) controller.abort(); });
    return () => { controller.abort(); unsubscribe(); };
  }, []);
  const [unconfirmed, setUnconfirmed] = useState<string[]>([]);
  const mutateViews = (mutation: () => void) => { try { mutation(); } catch (error) { setNotice(error instanceof PreferenceFileTooLargeError ? copy.fileTooLarge : copy.invalidFile); } };
  const refresh = () => { const result = loadSavedViewsResult(); setStored(result); setNotice(result.persisted ? copy.persisted : copy.sessionOnly); };
  const readOptions = async () => {
    if (lifetime.current?.signal.aborted) throw new Error('unavailable');
    const [models, endpoints, targets, keys] = await Promise.all([getSharedModels(0, true), getSharedEndpoints(0, true), getSharedConnectionOptions(0, true), getSharedProxyKeys(0, true)]);
    if (lifetime.current?.signal.aborted) throw new Error("cancelled");
    const segments = [...costSegments];
    let cursor: string | undefined;
    let snapshot: string | undefined;
    const seen = new Set<string>();
    do {
      const page = await api.stats.costSegments(cursor, lifetime.current?.signal);
      if (!Array.isArray(page.cost_segments) || typeof page.cost_segments_snapshot_hash !== 'string' || !page.cost_segments_snapshot_hash || (page.cost_segments_next_cursor !== null && typeof page.cost_segments_next_cursor !== 'string')) throw new Error('invalid_catalogue');
      if (snapshot && snapshot !== page.cost_segments_snapshot_hash) throw new Error('snapshot_changed');
      snapshot = page.cost_segments_snapshot_hash;
      segments.push(...page.cost_segments.map(segment => ({ value: segment.segment_key, label: segment.currency_code ?? segment.segment_key })));
      cursor = page.cost_segments_next_cursor ?? undefined;
      if (cursor && seen.has(cursor)) throw new Error('cursor_cycle');
      if (cursor) seen.add(cursor);
    } while (cursor);
    const result: ViewReferenceOptions = {
      model_id: models.filter(model => model.direct_request_enabled).map(model => ({ value: model.model_id, label: model.display_name || model.model_id })),
      resolved_target_model_id: models.map(model => ({ value: model.model_id, label: model.display_name || model.model_id })),
      endpoint_id: endpoints.map(endpoint => ({ value: String(endpoint.id), label: endpoint.name })),
      terminal_target_id: targets.map(target => ({ value: String(target.id), label: target.name || String(target.id) })),
      proxy_api_key_id: keys.map(key => ({ value: String(key.id), label: key.name })),
      client_rule_id: (filterOptionsLoaded ? filterOptions.clients : []).map(client => ({ value: String(client.client_rule_id), label: client.client_label })),
      cost_segment_key: segments,
    };
    if (lifetime.current?.signal.aborted) throw new Error("cancelled");
    setOptions(result); return result;
  };
  const restore = async (view: SavedRequestLogView, referencesConfirmed = false) => {
    setBusy(true); setNotice('');
    try {
      const refs = await readOptions();
      if (invalidViewReferences(view, refs).length || (!referencesConfirmed && VIEW_REFERENCE_KEYS.some(key => key !== "model_id" && key !== "resolved_target_model_id" && view.state[key]))) { setEditing(view); return; }
      actions.replaceState(applySavedView(view, actions.state)); setOpen(false);
    } catch { setNotice(copy.unavailable); } finally { setBusy(false); }
  };
  const importFile = async (file: File) => {
    setBusy(true); setNotice('');
    try {
      const views = parseRequestViewsFile(await readPreferenceFile(file));
      setPending(views);
      setUnconfirmed(views.flatMap(view => VIEW_REFERENCE_KEYS.filter(key => view.state[key] && key !== 'model_id' && key !== 'resolved_target_model_id').map(key => `${view.id}:${key}`)));
      try { await readOptions(); } catch { setOptions(null); setNotice(copy.unavailable); }
    } catch (error) { setPending([]); setNotice(error instanceof PreferenceFileTooLargeError ? copy.fileTooLarge : copy.invalidFile); } finally { setBusy(false); }
  };
  const updateReference = (id: string, key: ViewReferenceKey, value: string) => {
    setPending(views => views.map(view => view.id === id ? { ...view, state: { ...view.state, [key]: value } } : view));
    if (editing?.id === id) setEditing({ ...editing, state: { ...editing.state, [key]: value } });
    setUnconfirmed(keys => keys.filter(item => item !== `${id}:${key}`));
  };
  const referenceEditor = (view: SavedRequestLogView) => <div className="flex flex-col gap-2">{VIEW_REFERENCE_KEYS.filter(key => view.state[key]).map(key => <Field key={key}><FieldLabel>{copy.refs[key]}</FieldLabel><div className="flex gap-2"><Input aria-label={`${copy.refs[key]} ${view.name}`} value={view.state[key]} onChange={event => updateReference(view.id, key, event.target.value)} /><Button variant="outline" size="sm" onClick={() => updateReference(view.id, key, '')}>{copy.dropFilter}</Button></div><div className="flex flex-wrap gap-1">{options?.[key].slice(0, 20).map(option => <Button key={option.value} variant="ghost" size="sm" onClick={() => updateReference(view.id, key, option.value)}>{option.label} · {option.value}</Button>)}</div></Field>)}</div>;
  return <div className="w-full">
    <Button variant="outline" size="sm" aria-expanded={open} onClick={() => setOpen(value => !value)}>{messages.requestLogs.savedViewsLabel}</Button>
    {open && <section className="operator-section-surface mt-2 flex flex-col gap-2 p-3" data-testid="request-log-saved-views" aria-label={copy.requestViews}>
      {!stored.persisted && <OperatorCallout intent="warning" description={copy.sessionOnly} />}
      {stored.error && <OperatorCallout intent="warning" description={copy.readFailed} />}
      {notice && <p role="status">{notice}</p>}
      <p className="text-xs text-muted-foreground">{copy.safeFields}</p>
      <div className="flex flex-wrap items-end gap-2"><Field><FieldLabel>{messages.requestLogs.savedViewNamePlaceholder}</FieldLabel><Input aria-label={messages.requestLogs.savedViewNamePlaceholder} value={name} maxLength={80} onChange={event => setName(event.target.value)} /></Field><Button disabled={stored.error} onClick={() => { if (!name.trim()) { setNotice(copy.invalidName); return; } if (stored.views.some(view => view.name.toLowerCase() === name.trim().toLowerCase())) { setNotice(copy.nameConflict); return; } if (stored.views.length >= 20) { setNotice(copy.limit); return; } mutateViews(() => { saveRequestLogView(name, actions.state); setName(''); refresh(); }); }}>{messages.requestLogs.saveView}</Button>
      <Button variant="outline" onClick={() => mutateViews(() => downloadPreference('prism-request-views.json', requestViewsFile(stored.views)))}>{copy.exportFile}</Button><Field><FieldLabel>{copy.importView}</FieldLabel><Button variant="outline" disabled={busy} onClick={() => fileInput.current?.click()}>{copy.chooseFile}</Button><Input ref={fileInput} hidden aria-label={copy.importView} type="file" accept="application/json,.json" disabled={busy} onChange={event => { const file = event.target.files?.[0]; event.target.value = ''; if (file) void importFile(file); }} /></Field></div>
      {stored.views.map(view => <div key={view.id} className="flex flex-wrap items-center gap-2"><span>{view.name}</span><Button variant="outline" size="sm" disabled={busy} onClick={() => void restore(view)}>{copy.load}</Button><Button variant="ghost" size="sm" disabled={stored.error} onClick={() => mutateViews(() => { deleteRequestLogView(view.id); refresh(); })}>{copy.remove}</Button></div>)}
      {editing && <><OperatorCallout intent="warning" description={copy.fixReferences} />{referenceEditor(editing)}<Button disabled={stored.error || busy || !options || invalidViewReferences(editing, options).length > 0} onClick={() => mutateViews(() => { saveRequestLogView(editing.name, { ...actions.state, ...editing.state }); refresh(); void restore(editing, true); setEditing(null); })}>{copy.saveRepair}</Button><Button variant="outline" onClick={() => setEditing(null)}>{copy.cancel}</Button></>}
      {pending.length > 0 && <><h3>{copy.preview}</h3>{stored.error && <OperatorCallout intent="warning" description={copy.repairStorage} />}<Button variant="outline" disabled={busy} onClick={async () => { setBusy(true); try { await readOptions(); setNotice(''); } catch { setNotice(copy.unavailable); } finally { setBusy(false); } }}>{messages.requestLogs.refreshRequestLogs}</Button><OperatorCallout intent="warning" description={copy.fixReferences} />{pending.map(view => <div key={view.id} className="flex flex-col gap-2"><Field><FieldLabel>{copy.name}</FieldLabel><Input aria-label={`${copy.name} ${view.id}`} value={view.name} onChange={event => setPending(views => views.map(item => item.id === view.id ? { ...item, name: event.target.value } : item))} /></Field>{referenceEditor(view)}</div>)}<Button disabled={busy || !options || unconfirmed.length > 0 || pending.some(view => invalidViewReferences(view, options).length > 0)} onClick={() => { try { importSavedViews(pending); refresh(); setPending([]); } catch (error) { setNotice(error instanceof PreferenceFileTooLargeError ? copy.fileTooLarge : error instanceof Error && error.message === "limit" ? copy.limit : error instanceof Error && error.message === "invalid_name" ? copy.invalidName : copy.nameConflict); } }}>{copy.apply}</Button><Button variant="outline" onClick={() => setPending([])}>{copy.cancel}</Button></>}
    </section>}
  </div>;
}
