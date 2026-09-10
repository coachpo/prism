/** Bounded browser preferences. Failed writes remain usable in this tab and are explicit. */
export const MAX_PREFERENCE_FILE_BYTES = 256 * 1024;
export class PreferenceFileTooLargeError extends Error {
  constructor() { super('file_too_large'); this.name = 'PreferenceFileTooLargeError'; }
}
/** The budget includes UTF-8, JSON escaping, indentation, and the trailing newline. */
export function serializePreferenceFile(value: unknown): string {
  const text = JSON.stringify(value, null, 2) + "\n";
  if (new TextEncoder().encode(text).byteLength > MAX_PREFERENCE_FILE_BYTES) throw new PreferenceFileTooLargeError();
  return text;
}
const fallback = new Map<string, string>();
export function readPreference(key: string): { raw: string | null; persisted: boolean } {
  if (fallback.has(key)) return { raw: fallback.get(key)!, persisted: false };
  try { return { raw: localStorage.getItem(key), persisted: true }; }
  catch { return { raw: null, persisted: false }; }
}
export function writePreference(key: string, value: unknown): boolean {
  const raw = JSON.stringify(value);
  try { localStorage.setItem(key, raw); fallback.delete(key); return true; }
  catch { fallback.set(key, raw); return false; }
}
export function downloadPreference(filename: string, value: unknown): void {
  const url = URL.createObjectURL(new Blob([serializePreferenceFile(value)], { type: 'application/json' }));
  const link = document.createElement('a');
  link.href = url; link.download = filename; link.click();
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
export async function readPreferenceFile(file: File): Promise<string> {
  if (file.size > MAX_PREFERENCE_FILE_BYTES) throw new PreferenceFileTooLargeError();
  return file.text();
}
