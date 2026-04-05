import { getCurrentLocale } from "./format";
import type { Messages } from "./messages/en";
import { enMessages } from "./messages/en";
import { zhCNMessages } from "./messages/zh-CN";

export function getStaticMessages(): Messages {
  return getCurrentLocale() === "zh-CN" ? zhCNMessages : enMessages;
}

export function getStaticLocaleMessages(): Messages {
  return getStaticMessages();
}

function normalizeKnownLabel(label: string | null | undefined) {
  return label?.trim().toLocaleLowerCase() ?? "";
}

export function isKnownAllModelsLabel(label: string, key: string) {
  const normalizedLabel = normalizeKnownLabel(label);
  return (
    key === "all" ||
    normalizedLabel === normalizeKnownLabel(enMessages.statistics.allModels) ||
    normalizedLabel === normalizeKnownLabel(getStaticMessages().statistics.allModels)
  );
}

export function isKnownUnknownEndpointLabel(label: string) {
  const normalizedLabel = normalizeKnownLabel(label);
  return (
    normalizedLabel === normalizeKnownLabel(enMessages.modelDetail.unknownEndpoint) ||
    normalizedLabel === normalizeKnownLabel(getStaticMessages().modelDetail.unknownEndpoint)
  );
}

export function isKnownUnknownProxyApiKeyLabel(label: string | null) {
  const normalizedLabel = normalizeKnownLabel(label);
  return (
    !label ||
    normalizedLabel === normalizeKnownLabel(enMessages.statistics.unknownProxyApiKey) ||
    normalizedLabel === normalizeKnownLabel(getStaticMessages().statistics.unknownProxyApiKey)
  );
}

export function isKnownUnknownVendorLabel(label: string | null | undefined) {
  const normalizedLabel = normalizeKnownLabel(label);
  return (
    !label ||
    normalizedLabel === normalizeKnownLabel(enMessages.modelsUi.unknownVendor) ||
    normalizedLabel === normalizeKnownLabel(getStaticMessages().modelsUi.unknownVendor)
  );
}
