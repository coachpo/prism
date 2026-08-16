// View-label mapping for payload views (kept out of the component file so
// fast refresh works).
import type { PayloadViewKind } from "./payloadDocumentViewModel";

export interface PayloadViewMessages {
  messageView: string;
  jsonEventsView: string;
  rawSseView: string;
  jsonView: string;
  rawTextView: string;
  unparseableView: string;
}

export function viewLabel(kind: PayloadViewKind, messages: PayloadViewMessages): string {
  switch (kind) {
    case "transcript":
      return messages.messageView;
    case "json_events":
      return messages.jsonEventsView;
    case "raw_sse":
      return messages.rawSseView;
    case "json":
      return messages.jsonView;
    case "raw_text":
      return messages.rawTextView;
    case "unparseable":
      return messages.unparseableView;
    default:
      return kind;
  }
}
