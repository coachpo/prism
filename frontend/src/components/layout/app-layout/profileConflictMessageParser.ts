export function parseConflictMessage(
  error: unknown,
  localizedLimitReachedMessage: string,
): string | null {
  if (!(error instanceof Error)) return null;
  if (error.message === localizedLimitReachedMessage) {
    return localizedLimitReachedMessage;
  }
  if (error.message.includes("409") || error.message.toLowerCase().includes("conflict")) {
    return error.message;
  }
  return null;
}
