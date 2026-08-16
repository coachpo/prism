export type ApiFamily = "openai" | "anthropic" | "gemini";

export interface Vendor {
  id: number;
  key: string;
  name: string;
  description: string | null;
  icon_key: string | null;
}
