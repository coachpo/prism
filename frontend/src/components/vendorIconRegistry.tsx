import type { ComponentType, SVGProps } from "react";
import type { VendorIconKey } from "@/lib/types";
import {
  AlibabaVendorIcon,
  AnthropicVendorIcon,
  AWSVendorIcon,
  AzureVendorIcon,
  BaiduVendorIcon,
  CohereVendorIcon,
  DeepseekVendorIcon,
  GoogleVendorIcon,
  HuggingfaceVendorIcon,
  KimiVendorIcon,
  MetaVendorIcon,
  MistralVendorIcon,
  NvidiaVendorIcon,
  OpenAIVendorIcon,
  OpenrouterVendorIcon,
  PerplexityVendorIcon,
  TencentVendorIcon,
  XAIVendorIcon,
  ZhipuVendorIcon,
} from "@/components/vendorIconComponents";

type VendorIconComponent = ComponentType<SVGProps<SVGSVGElement>>;

export interface VendorIconPresetOption {
  icon_key: VendorIconKey;
  label: string;
  Icon: VendorIconComponent;
}

export const vendorIconRegistry: Record<string, VendorIconPresetOption> = {
  openai: { icon_key: "openai", label: "OpenAI", Icon: OpenAIVendorIcon },
  anthropic: { icon_key: "anthropic", label: "Anthropic", Icon: AnthropicVendorIcon },
  google: { icon_key: "google", label: "Google", Icon: GoogleVendorIcon },
  deepseek: { icon_key: "deepseek", label: "DeepSeek", Icon: DeepseekVendorIcon },
  zhipu: { icon_key: "zhipu", label: "Z.ai", Icon: ZhipuVendorIcon },
  nvidia: { icon_key: "nvidia", label: "NVIDIA", Icon: NvidiaVendorIcon },
  aws: { icon_key: "aws", label: "AWS", Icon: AWSVendorIcon },
  azure: { icon_key: "azure", label: "Microsoft/Azure", Icon: AzureVendorIcon },
  meta: { icon_key: "meta", label: "Meta", Icon: MetaVendorIcon },
  mistral: { icon_key: "mistral", label: "Mistral", Icon: MistralVendorIcon },
  cohere: { icon_key: "cohere", label: "Cohere", Icon: CohereVendorIcon },
  huggingface: { icon_key: "huggingface", label: "Hugging Face", Icon: HuggingfaceVendorIcon },
  perplexity: { icon_key: "perplexity", label: "Perplexity", Icon: PerplexityVendorIcon },
  xai: { icon_key: "xai", label: "xAI", Icon: XAIVendorIcon },
  openrouter: { icon_key: "openrouter", label: "OpenRouter", Icon: OpenrouterVendorIcon },
  alibaba: { icon_key: "alibaba", label: "Alibaba Cloud", Icon: AlibabaVendorIcon },
  tencent: { icon_key: "tencent", label: "Tencent", Icon: TencentVendorIcon },
  baidu: { icon_key: "baidu", label: "Baidu", Icon: BaiduVendorIcon },
  kimi: { icon_key: "kimi", label: "Kimi", Icon: KimiVendorIcon },
};

export const vendorIconPresetOptions = Object.values(vendorIconRegistry);

export function getVendorIconPreset(iconKey: string | null | undefined): VendorIconPresetOption | null {
  if (!iconKey) {
    return null;
  }

  return vendorIconRegistry[iconKey.trim().toLowerCase()] ?? null;
}
