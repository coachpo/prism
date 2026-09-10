import { PreferenceFileTooLargeError, readPreferenceFile, serializePreferenceFile } from './storage';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { exportPresetsFile, loadExportPresets, parseExportPresets, resolveExportPreset, writeExportPresets, EXPORT_PRESET_KEY } from './exportPresets';
const preset = { name: '工作', client: 'opencode' as const, modelIds: ['alpha'], gatewayOrigin: 'http://localhost:8080', providerId: 'prism' };
beforeEach(() => { vi.stubGlobal('localStorage', { getItem: () => null, setItem: vi.fn() }); });
describe('export preference trust boundary', () => {
  it('round trips safe fields and resolves semantic model identity against current independent client facts', () => {
    const restored = parseExportPresets(JSON.stringify(exportPresetsFile([preset])))[0];
    expect(resolveExportPreset(restored, [{ model_config_id: 92, model_id: 'alpha', selectable: true }]).selected).toEqual(new Set([92]));
    expect(resolveExportPreset(restored, [{ model_config_id: 92, model_id: 'alpha', selectable: false }]).invalid).toEqual(['alpha']);
    expect(resolveExportPreset(restored, [{ model_config_id: 1, model_id: 'renamed', selectable: true }]).invalid).toEqual(['alpha']);
  });
  it('rejects Pi preset references without affirmative readiness and reports them for repair', () => {
    const pi = { ...preset, client: 'pi' as const, modelIds: ['ready', 'blocked', 'missing'] };
    const restored = resolveExportPreset(pi, [
      { model_config_id: 1, model_id: 'ready', selectable: true, readiness: { status: 'ready' } },
      { model_config_id: 2, model_id: 'blocked', selectable: true, readiness: { status: 'blocked' } },
      { model_config_id: 3, model_id: 'missing', selectable: true },
    ]);
    expect(restored.selected).toEqual(new Set([1]));
    expect(restored.invalid).toEqual(['blocked', 'missing']);
  });
  it('rejects credentials in destination, unknown fields, future versions and damaged files', () => {
    for (const value of [
      { ...exportPresetsFile([preset]), version: 2 },
      { ...exportPresetsFile([preset]), presets: [{ ...preset, apiKey: 'secret' }] },
      { ...exportPresetsFile([preset]), presets: [{ ...preset, gatewayOrigin: 'https://user:pass@example.com' }] },
      { ...exportPresetsFile([preset]), presets: [{ ...preset, gatewayOrigin: 'https://example.com?key=secret' }] },
    ]) expect(() => parseExportPresets(JSON.stringify(value))).toThrow();
    expect(() => parseExportPresets('{broken')).toThrow();
  });
  it('rejects an oversized aggregate at the writer without replacing last-good, while legacy large data remains readable', async () => {
    const store = new Map<string, string>();
    vi.stubGlobal('localStorage', { getItem: (key: string) => store.get(key) ?? null, setItem: (key: string, raw: string) => store.set(key, raw) });
    writeExportPresets([preset]);
    const good = store.get(EXPORT_PRESET_KEY);
    const oversized = Array.from({ length: 3 }, (_, index) => ({ ...preset, name: `large-${index}`, modelIds: Array.from({ length: 500 }, () => '\u0001'.repeat(256)) }));
    expect(() => writeExportPresets(oversized)).toThrow(PreferenceFileTooLargeError);
    expect(store.get(EXPORT_PRESET_KEY)).toBe(good);
    const text = serializePreferenceFile(exportPresetsFile([preset]));
    const restored = await readPreferenceFile({ size: new TextEncoder().encode(text).byteLength, text: async () => text } as File);
    expect(parseExportPresets(restored)).toEqual([preset]);
    store.set(EXPORT_PRESET_KEY, JSON.stringify(exportPresetsFile(oversized)));
    expect(loadExportPresets()).toMatchObject({ error: false, presets: oversized });
  });
  it('reports failed persistence and retains preferences only in memory', () => {
    vi.stubGlobal('localStorage', { getItem: () => null, setItem: () => { throw new Error('quota'); } });
    expect(writeExportPresets([preset])).toBe(false);
    expect(loadExportPresets()).toEqual({ presets: [preset], persisted: false, error: false });
  });
});
