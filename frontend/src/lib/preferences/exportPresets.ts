import { z } from 'zod';
import { readPreference, serializePreferenceFile, writePreference } from './storage';
export const EXPORT_PRESET_KEY = 'prism.export-presets.v1';
const destination = z.string().max(2048).refine(value => {
  try { const url = new URL(value); return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password && !url.search && !url.hash && (url.pathname === '/' || !url.pathname); }
  catch { return false; }
});
export const exportPresetSchema = z.object({
  name: z.string().trim().min(1).max(80),
  client: z.enum(['pi', 'opencode']),
  modelIds: z.array(z.string().min(1).max(256)).max(500),
  gatewayOrigin: destination,
  providerId: z.string().trim().min(1).max(100).refine(value => !value.includes('/')),
}).strict();
const fileSchema = z.object({ format: z.literal('prism.export-presets'), version: z.literal(1), presets: z.array(exportPresetSchema).max(20) }).strict();
export type ExportPreset = z.infer<typeof exportPresetSchema>;
export function exportPresetsFile(presets: ExportPreset[]) { return fileSchema.parse({ format: 'prism.export-presets', version: 1, presets }); }
export function parseExportPresets(raw: string): ExportPreset[] { return fileSchema.parse(JSON.parse(raw)).presets; }
export function loadExportPresets(): { presets: ExportPreset[]; persisted: boolean; error: boolean } {
  const stored = readPreference(EXPORT_PRESET_KEY);
  try { return { presets: stored.raw ? parseExportPresets(stored.raw) : [], persisted: stored.persisted, error: false }; }
  catch { return { presets: [], persisted: stored.persisted, error: true }; }
}
export function writeExportPresets(presets: ExportPreset[]): boolean {
  const file = exportPresetsFile(presets);
  serializePreferenceFile(file);
  return writePreference(EXPORT_PRESET_KEY, file);
}
export interface ExportPresetModelReference { model_config_id: number; model_id: string; selectable: boolean; readiness?: { status: string }; }
export function resolveExportPreset(preset: ExportPreset, models: ExportPresetModelReference[]) {
  const selected = new Set<number>();
  const invalid: string[] = [];
  for (const id of preset.modelIds) {
    const matches = models.filter(model => model.model_id === id && model.selectable && (preset.client !== "pi" || model.readiness?.status === "ready"));
    if (matches.length === 1) selected.add(matches[0].model_config_id); else invalid.push(id);
  }
  return { selected, invalid };
}
