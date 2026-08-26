import type { ApiFamily } from "@/lib/types";

const API_FAMILY_LABELS: Record<ApiFamily, string> = {
  openai: "OpenAI",
  anthropic: "Anthropic",
  gemini: "Gemini",
};

export function formatApiFamily(apiFamily: string): string {
  return API_FAMILY_LABELS[apiFamily as ApiFamily] ?? "-";
}
