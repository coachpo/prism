type ActivityCursorItem = {
  usage_event_id: string;
};

/** Activity continuation is keyed by the last retained usage-event identity. */
export function nextObserveActivityCursor(items: readonly ActivityCursorItem[], hasMore: boolean): string | null {
  return hasMore ? (items.at(-1)?.usage_event_id ?? null) : null;
}
