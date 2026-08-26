import { parse, type ParseError } from "jsonc-parser";
import type { ExportPlatform } from "./exportTypes";

/**
 * In-browser safe extraction of existing Pi / OpenCode client configuration
 * files. Parsed content never leaves the browser: raw file text is read,
 * parsed locally, credential-shaped material is dropped immediately, and only
 * non-sensitive enhancement fields survive for per-item operator review.
 *
 * Supported inputs:
 *  - Pi 0.84.3 models.json ({providers: {...}})
 *  - Pi provider catalog cache models-store.json ({providerId: entry})
 *  - OpenCode JSON/JSONC config ({provider: {...}}), parsed by jsonc-parser;
 *    the raw text is never uploaded.
 */

export interface ExtractedHeaderCandidate {
  id: string;
  platform: ExportPlatform;
  providerId: string;
  /** Absent means the header was provider-wide in the uploaded file. */
  modelId?: string;
  /** Header name; values are shown to the operator for explicit approval. */
  name: string;
  value: string;
}

export interface ExtractedModelEnhancement {
  /** Client model id this enhancement came from. */
  clientId: string;
  /** Exact model id from the uploaded model key/id; slashes are preserved. */
  modelId: string;
  providerId: string;
  platform: ExportPlatform;
  /** Platform-keyed fields proposed as fill-missing enhancements. */
  fields: Record<string, unknown>;
}

export interface ExtractionResult {
  sourceKind: "pi-models" | "pi-store" | "opencode";
  models: ExtractedModelEnhancement[];
  /** Headers awaiting per-item confirmation; sensitive ones were dropped. */
  headerCandidates: ExtractedHeaderCandidate[];
  /** Human-readable notes about what was dropped or skipped. */
  notes: string[];
}

const NON_SECRET_EXACT_NAMES = new Set([
  "access-control-allow-credentials",
]);

const SENSITIVE_EXACT_NAMES = new Set([
  "authorization",
  "proxy-authorization",
  "cookie",
  "set-cookie",
  "x-api-key",
  "x-goog-api-key",
  "api-key",
  "apikey",
  "openai-api-key",
  "anthropic-api-key",
  "x-trace-id",
  "x-upstream-trace",
]);

// Exact names and the non-secret exception mirror backend/domain/safediag.
// Token-bearing JSON metadata such as maxTokens is legitimate client config,
// so token/session credential compounds are matched through the collapsed-name
// list below instead of a generic fragment match.
const SENSITIVE_NAME_FRAGMENTS = [
  "api-key",
  "api_key",
  "secret",
  "credential",
  "password",
  "passwd",
  "private-key",
  "private_key",
  "jwt",
];

/** Keys that always fail closed during extraction. */
export function keyLooksSensitive(key: string): boolean {
  const normalized = key.trim().toLowerCase();
  if (!normalized) return true;
  if (NON_SECRET_EXACT_NAMES.has(normalized)) return false;
  if (SENSITIVE_EXACT_NAMES.has(normalized)) return true;
  if (
    SENSITIVE_NAME_FRAGMENTS.some((fragment) =>
      normalized.includes(fragment),
    )
  ) {
    return true;
  }

  const collapsed = normalized.replace(/[^a-z0-9]/g, "");
  return [
    "apikey",
    "proxykey",
    "authtoken",
    "accesstoken",
    "refreshtoken",
    "sessiontoken",
    "idtoken",
    "oauthtoken",
    "sessionkey",
    "accesskey",
    "privatekey",
    "bearer",
    "clientsecret",
    "signature",
    "satoken",
  ].some((needle) => collapsed.includes(needle)) || collapsed === "token";
}

function parseJsonLike(text: string): Record<string, unknown> | null {
  const trimmed = text.trim();
  if (!trimmed) return null;
  const errors: ParseError[] = [];
  const parsed = parse(trimmed, errors, {
    allowTrailingComma: true,
    disallowComments: false,
    allowEmptyContent: false,
  }) as unknown;
  if (errors.length > 0) return null;
  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
    return parsed as Record<string, unknown>;
  }
  return null;
}

function detectSourceKind(
  parsed: Record<string, unknown>,
): "pi-models" | "pi-store" | "opencode" | null {
  if (parsed.provider && typeof parsed.provider === "object") return "opencode";
  if (parsed.providers && typeof parsed.providers === "object")
    return "pi-models";
  // models-store.json maps provider ids straight to catalog entries.
  const values = Object.values(parsed);
  if (
    values.length > 0 &&
    values.every(
      (value) => value && typeof value === "object" && !Array.isArray(value),
    )
  ) {
    const hasCatalogShape = values.some(
      (value) => "models" in (value as Record<string, unknown>),
    );
    if (hasCatalogShape) return "pi-store";
  }
  return null;
}

/** A JSON value tree after sensitive-key stripping. */
export type JsonValue =
  | string
  | number
  | boolean
  | null
  | JsonValue[]
  | { [key: string]: JsonValue };

/**
 * Recursively drops any object entry whose key looks like credential
 * material. Values that survive are structural metadata only.
 */
export function dropSensitiveDeep(value: unknown): JsonValue {
  if (Array.isArray(value)) return value.map(dropSensitiveDeep);
  if (value && typeof value === "object") {
    const cleaned: Record<string, JsonValue> = {};
    for (const [key, child] of Object.entries(
      value as Record<string, unknown>,
    )) {
      if (keyLooksSensitive(key)) continue;
      cleaned[key] = dropSensitiveDeep(child);
    }
    return cleaned;
  }
  if (
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean" ||
    value === null
  ) {
    return value;
  }
  return null;
}

/** Fields worth carrying over as Pi model-level enhancements. */
function piModelFields(
  entry: Record<string, unknown>,
  notes: string[],
): Record<string, unknown> | null {
  const allowed = ["thinkingLevelMap", "compat"];
  const fields: Record<string, unknown> = {};
  for (const key of allowed) {
    if (key in entry && entry[key] !== null)
      fields[key] = dropSensitiveDeep(entry[key]);
  }
  if ("samplingParams" in entry) {
    notes.push(
      `模型 ${String(entry.id ?? "")} 的 samplingParams 属于请求改写面，已整体丢弃。`,
    );
  }
  return Object.keys(fields).length > 0 ? fields : null;
}

/** Fields worth carrying over as OpenCode model-level enhancements. */
function opencodeModelFields(
  entry: Record<string, unknown>,
): Record<string, unknown> | null {
  const allowed = ["interleaved", "variants"];
  const fields: Record<string, unknown> = {};
  for (const key of allowed) {
    if (key in entry && entry[key] !== null)
      fields[key] = dropSensitiveDeep(entry[key]);
  }
  if (entry.options && typeof entry.options === "object") {
    const sanitized = dropSensitiveDeep(entry.options);
    if (sanitized && typeof sanitized === "object" && !Array.isArray(sanitized)) {
      // options.headers is reviewed through headerCandidates below; retaining
      // it here would bypass item-by-item approval.
      const safeOptions = { ...sanitized };
      delete safeOptions.headers;
      delete safeOptions.baseURL;
      delete safeOptions.base_url;
      if (Object.keys(safeOptions).length > 0) fields.options = safeOptions;
    }
  }
  return Object.keys(fields).length > 0 ? fields : null;
}

/**
 * Parses one uploaded client file into reviewable enhancement candidates.
 * Throws when the payload shape matches no supported format.
 */
export function extractClientConfig(rawText: string): ExtractionResult {
  const parsed = parseJsonLike(rawText);
  if (!parsed) throw new Error("文件不是合法的 JSON/JSONC 配置");
  const kind = detectSourceKind(parsed);
  if (!kind)
    throw new Error(
      "无法识别为 Pi models.json、models-store.json 或 OpenCode 配置",
    );

  const notes: string[] = [];
  const models: ExtractedModelEnhancement[] = [];
  const headerCandidates: ExtractedHeaderCandidate[] = [];

  const collectHeaders = (
    container: unknown,
    context: {
      platform: ExportPlatform;
      providerId: string;
      modelId?: string;
    },
  ) => {
    if (!container || typeof container !== "object") return;
    for (const [name, value] of Object.entries(
      container as Record<string, unknown>,
    )) {
      if (typeof value !== "string" || !value) continue;
      if (keyLooksSensitive(name)) {
        notes.push(`请求头 ${name} 命中敏感规则，已立即丢弃。`);
        continue;
      }
      headerCandidates.push({
        id: JSON.stringify([
          context.platform,
          context.providerId,
          context.modelId ?? null,
          name,
          headerCandidates.length,
        ]),
        ...context,
        name,
        value,
      });
    }
  };

  if (kind === "opencode") {
    const providers = (parsed.provider ?? {}) as Record<string, unknown>;
    for (const [providerId, providerValue] of Object.entries(providers)) {
      if (!providerValue || typeof providerValue !== "object") continue;
      const provider = providerValue as Record<string, unknown>;
      const providerModels = (provider.models ?? {}) as Record<string, unknown>;
      for (const [modelId, modelValue] of Object.entries(providerModels)) {
        if (!modelValue || typeof modelValue !== "object") continue;
        const model = modelValue as Record<string, unknown>;
        const fields = opencodeModelFields(model);
        models.push({
          clientId: `${providerId}/${modelId}`,
          modelId,
          providerId,
          platform: "opencode",
          fields: fields ?? {},
        });
        collectHeaders(model.headers, {
          platform: "opencode",
          providerId,
          modelId,
        });
        if (model.options && typeof model.options === "object") {
          collectHeaders(
            (model.options as Record<string, unknown>).headers,
            { platform: "opencode", providerId, modelId },
          );
        }
      }
      // Provider-level options are never inherited; only model-level options
      // pass through the recursive sensitive/locked-field sanitizer above.
      collectHeaders(provider.headers, {
        platform: "opencode",
        providerId,
      });
      if ("options" in provider && provider.options) {
        notes.push(
          `提供方 ${providerId} 的 options 已整体丢弃；只继承模型级安全 options。`,
        );
      }
      if (provider.env)
        notes.push(
          `提供方 ${providerId} 的 env 键槽已忽略（由 Prism 导出统一生成）。`,
        );
    }
  } else if (kind === "pi-models") {
    const providers = (parsed.providers ?? {}) as Record<string, unknown>;
    for (const [providerId, providerValue] of Object.entries(providers)) {
      if (!providerValue || typeof providerValue !== "object") continue;
      const provider = providerValue as Record<string, unknown>;
      const providerModels = Array.isArray(provider.models)
        ? provider.models
        : [];
      for (const modelValue of providerModels) {
        if (!modelValue || typeof modelValue !== "object") continue;
        const entry = modelValue as Record<string, unknown>;
        const modelId = String(entry.id ?? "");
        const fields = piModelFields(entry, notes);
        if (modelId)
          models.push({
            clientId: `${providerId}/${modelId}`,
            modelId,
            providerId,
            platform: "pi",
            fields: fields ?? {},
          });
        collectHeaders(entry.headers, {
          platform: "pi",
          providerId,
          modelId,
        });
      }
      collectHeaders(provider.headers, { platform: "pi", providerId });
      if ("apiKey" in provider)
        notes.push(`提供方 ${providerId} 的 apiKey 已立即丢弃。`);
    }
  } else {
    for (const [providerId, providerValue] of Object.entries(parsed)) {
      if (!providerValue || typeof providerValue !== "object") continue;
      const provider = providerValue as Record<string, unknown>;
      const providerModels = (provider.models ?? {}) as Record<string, unknown>;
      for (const [modelId, modelValue] of Object.entries(providerModels)) {
        if (!modelValue || typeof modelValue !== "object") continue;
        const entry: Record<string, unknown> = {
          ...(modelValue as Record<string, unknown>),
          id: modelId,
        };
        const fields = piModelFields(entry, notes);
        models.push({
          clientId: `${providerId}/${modelId}`,
          modelId,
          providerId,
          platform: "pi",
          fields: fields ?? {},
        });
        collectHeaders(entry.headers, {
          platform: "pi",
          providerId,
          modelId,
        });
      }
      collectHeaders(provider.headers, { platform: "pi", providerId });
    }
    notes.push(
      "models-store.json 仅提取目录元数据；密钥与缓存时间戳不参与导出。",
    );
  }

  return { sourceKind: kind, models, headerCandidates, notes };
}
