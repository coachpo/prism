import { useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Field, FieldLabel } from '@/components/ui/field';
import { OperatorCallout } from '@/shared/design-system';
import { useLocale } from '@/i18n/useLocale';
import { exportPresetSchema, exportPresetsFile, loadExportPresets, parseExportPresets, resolveExportPreset, writeExportPresets, type ExportPreset, type ExportPresetModelReference } from '@/lib/preferences/exportPresets';
import { downloadPreference, PreferenceFileTooLargeError, readPreferenceFile } from '@/lib/preferences/storage';
interface Props {
  client: 'pi' | 'opencode';
  models: ExportPresetModelReference[];
  selectedIds: Set<number>;
  gatewayOrigin: string;
  providerId: string;
  blocked: boolean;
  onApply: (selected: Set<number>, destination: { gatewayOrigin: string; providerId: string }) => void;
}
export function ExportPresetsPanel(props: Props) {
  const { messages } = useLocale(); const copy = messages.preferences;
  const fileInput = useRef<HTMLInputElement>(null);
  const [loaded, setLoaded] = useState(loadExportPresets);
  const [name, setName] = useState('');
  const [notice, setNotice] = useState('');
  const [incoming, setIncoming] = useState<ExportPreset[]>([]);
  const [pending, setPending] = useState<ExportPreset | null>(null);
  const persist = (presets: ExportPreset[]) => {
    let persisted: boolean;
    try { persisted = writeExportPresets(presets); }
    catch (error) { setNotice(error instanceof PreferenceFileTooLargeError ? copy.fileTooLarge : copy.invalidPreset); return false; }
    setLoaded({ presets, persisted, error: false });
    setNotice(persisted ? copy.persisted : copy.sessionOnly);
    return true;
  };
  const save = () => {
    const parsed = exportPresetSchema.safeParse({ name: name.trim(), client: props.client, modelIds: props.models.filter(model => props.selectedIds.has(model.model_config_id)).map(model => model.model_id), gatewayOrigin: props.gatewayOrigin, providerId: props.providerId });
    if (!parsed.success) { setNotice(copy.invalidPreset); return; }
    if (loaded.presets.some(item => item.name.toLowerCase() === name.trim().toLowerCase() && item.client === props.client)) { setNotice(copy.nameConflict); return; }
    if (loaded.presets.length >= 20) { setNotice(copy.limit); return; }
    if (persist([...loaded.presets, parsed.data])) setName('');
  };
  const apply = (preset: ExportPreset, discard = false) => {
    if (props.blocked) { setNotice(copy.unavailable); return; }
    const resolved = resolveExportPreset(preset, props.models);
    if (resolved.invalid.length && !discard) { setPending(preset); return; }
    props.onApply(resolved.selected, preset); setPending(null);
  };
  const importItems = () => {
    if (incoming.some((item, index) => !item.name.trim() || item.name.length > 80 || [...loaded.presets, ...incoming.slice(0, index)].some(other => other.client === item.client && other.name.toLowerCase() === item.name.trim().toLowerCase()))) { setNotice(copy.nameConflict); return; }
    if (loaded.presets.length + incoming.length > 20) { setNotice(copy.limit); return; }
    if (persist([...loaded.presets, ...incoming])) setIncoming([]);
  };
  return <section className="operator-section-surface flex flex-col gap-2 p-3" aria-label={copy.title}>
    <h2>{copy.title}</h2><p className="text-xs text-muted-foreground">{copy.safeFields}</p>
    {!loaded.persisted && <OperatorCallout intent="warning" description={copy.sessionOnly} />}
    {loaded.error && <OperatorCallout intent="warning" description={copy.readFailed} />}
    {notice && <p role="status">{notice}</p>}
    <div className="flex flex-wrap items-end gap-2"><Field><FieldLabel>{copy.name}</FieldLabel><Input aria-label={copy.name} value={name} maxLength={80} onChange={event => setName(event.target.value)} /></Field><Button variant="outline" onClick={save} disabled={props.blocked || loaded.error}>{copy.save}</Button>
      <Button variant="outline" onClick={() => { try { downloadPreference('prism-export-presets.json', exportPresetsFile(loaded.presets)); } catch (error) { setNotice(error instanceof PreferenceFileTooLargeError ? copy.fileTooLarge : copy.invalidFile); } }}>{copy.exportFile}</Button>
      <Field><FieldLabel>{copy.importFile}</FieldLabel><Button variant="outline" onClick={() => fileInput.current?.click()}>{copy.chooseFile}</Button><Input ref={fileInput} hidden aria-label={copy.importFile} type="file" accept="application/json,.json" onChange={async event => { const file = event.target.files?.[0]; event.target.value = ''; if (!file) return; try { setIncoming(parseExportPresets(await readPreferenceFile(file))); setNotice(''); } catch (error) { setNotice(error instanceof PreferenceFileTooLargeError ? copy.fileTooLarge : copy.invalidFile); } }} /></Field>
    </div>
    {loaded.presets.filter(preset => preset.client === props.client).map(preset => <div key={preset.name} className="flex flex-wrap items-center gap-2"><span>{preset.name}</span><Button variant="outline" size="sm" onClick={() => apply(preset)} disabled={props.blocked}>{copy.load}</Button><Button variant="ghost" size="sm" onClick={() => persist(loaded.presets.filter(item => item !== preset))}>{copy.remove}</Button></div>)}
    {pending && <OperatorCallout intent="warning" description={`${copy.invalidModels} ${resolveExportPreset(pending, props.models).invalid.join(', ')}`} action={<div className="flex gap-2"><Button variant="outline" onClick={() => apply(pending, true)} disabled={props.blocked}>{copy.dropModels}</Button><Button variant="ghost" onClick={() => setPending(null)}>{copy.cancel}</Button></div>} />}
    {incoming.length > 0 && <div className="flex flex-col gap-2"><h3>{copy.preview}</h3>{loaded.error && <OperatorCallout intent="warning" description={copy.repairStorage} />}{incoming.map((preset, index) => <Field key={index}><FieldLabel>{preset.client} · {preset.modelIds.join(', ')} · {preset.gatewayOrigin} · {preset.providerId}</FieldLabel><Input aria-label={`${copy.name} ${index + 1}`} value={preset.name} onChange={event => setIncoming(items => items.map((item, position) => position === index ? { ...item, name: event.target.value } : item))} /></Field>)}<div className="flex gap-2"><Button onClick={importItems}>{copy.apply}</Button><Button variant="outline" onClick={() => setIncoming([])}>{copy.cancel}</Button></div></div>}
  </section>;
}
