import { zhCNMessages, type Messages } from "./messages";

export function getStaticMessages(): Messages {
  return zhCNMessages;
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
    normalizedLabel === normalizeKnownLabel(getStaticMessages().statistics.allModels)
  );
}

export function isKnownUnknownEndpointLabel(label: string) {
  const normalizedLabel = normalizeKnownLabel(label);
  return (
    normalizedLabel === normalizeKnownLabel(getStaticMessages().modelDetail.unknownEndpoint)
  );
}

export function isKnownUnknownProxyApiKeyLabel(label: string | null) {
  const normalizedLabel = normalizeKnownLabel(label);
  return (
    !label ||
    normalizedLabel === normalizeKnownLabel(getStaticMessages().statistics.unknownProxyApiKey)
  );
}

export function isKnownUnknownVendorLabel(label: string | null | undefined) {
  const normalizedLabel = normalizeKnownLabel(label);
  return (
    !label ||
    normalizedLabel === normalizeKnownLabel(getStaticMessages().modelsUi.unknownVendor)
  );
}
