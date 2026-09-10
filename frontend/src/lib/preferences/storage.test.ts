import { describe, expect, it, vi } from 'vitest';
import { downloadPreference, MAX_PREFERENCE_FILE_BYTES, PreferenceFileTooLargeError, readPreferenceFile, serializePreferenceFile } from './storage';

describe('portable preference byte budget', () => {
  it('accepts the exact encoded boundary and rejects one extra byte', async () => {
    const overhead = new TextEncoder().encode(serializePreferenceFile({ text: '' })).byteLength;
    const text = serializePreferenceFile({ text: 'a'.repeat(MAX_PREFERENCE_FILE_BYTES - overhead) });
    const bytes = new TextEncoder().encode(text).byteLength;
    expect(bytes).toBe(MAX_PREFERENCE_FILE_BYTES);
    await expect(readPreferenceFile({ size: bytes, text: async () => text } as File)).resolves.toBe(text);
    expect(() => serializePreferenceFile({ text: 'a'.repeat(MAX_PREFERENCE_FILE_BYTES - overhead + 1) })).toThrow(PreferenceFileTooLargeError);
    await expect(readPreferenceFile({ size: bytes + 1 } as File)).rejects.toBeInstanceOf(PreferenceFileTooLargeError);
  });
  it('counts JSON escaping and UTF-8 instead of source string length, before creating a download', () => {
    const value = { text: '\u0001'.repeat(44000) + '模型' };
    expect(value.text.length).toBeLessThan(MAX_PREFERENCE_FILE_BYTES);
    expect(() => serializePreferenceFile(value)).toThrow(PreferenceFileTooLargeError);
    const createUrl = vi.fn();
    vi.stubGlobal('URL', { createObjectURL: createUrl });
    try { expect(() => downloadPreference('large.json', value)).toThrow(PreferenceFileTooLargeError); expect(createUrl).not.toHaveBeenCalled(); }
    finally { vi.unstubAllGlobals(); }
  });
});
