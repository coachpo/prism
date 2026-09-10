import { z } from 'zod';
import { requestViewsFileData, validateSavedViewState, type SavedRequestLogView } from './requestLogSavedViews';
const fileSchema = z.object({
  format: z.literal('prism.request-views'), version: z.literal(1),
  views: z.array(z.object({ name: z.string().trim().min(1).max(80), state: z.unknown() }).strict()).max(20),
}).strict();
export const VIEW_REFERENCE_KEYS = ['model_id', 'resolved_target_model_id', 'endpoint_id', 'terminal_target_id', 'client_rule_id', 'proxy_api_key_id', 'cost_segment_key'] as const;
export type ViewReferenceKey = typeof VIEW_REFERENCE_KEYS[number];
export function requestViewsFile(views: SavedRequestLogView[]) {
  return fileSchema.parse(requestViewsFileData(views));
}
export function parseRequestViewsFile(raw: string): SavedRequestLogView[] {
  const file = fileSchema.parse(JSON.parse(raw));
  return file.views.map((view, index) => ({ id: `import-${index}`, name: view.name, state: validateSavedViewState(view.state), createdAt: new Date().toISOString(), updatedAt: new Date().toISOString() }));
}
export type ViewReferenceOptions = Record<ViewReferenceKey, { value: string; label: string }[]>;
export function invalidViewReferences(view: SavedRequestLogView, options: ViewReferenceOptions): ViewReferenceKey[] {
  return VIEW_REFERENCE_KEYS.filter(key => view.state[key] && !view.state[key].split(',').every(value => options[key].some(option => option.value === value)));
}
