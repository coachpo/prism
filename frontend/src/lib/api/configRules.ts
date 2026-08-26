import type {
  HeaderBlocklistRule,
  HeaderBlocklistRuleCreate,
  HeaderBlocklistRuleUpdate,
  UserAgentClientRule,
  UserAgentClientRuleCreate,
  UserAgentClientRuleUpdate,
} from "../types";
import { request } from "./request";

export const config = {
  headerBlocklistRules: {
    list: (includeDisabled = true) =>
      request<HeaderBlocklistRule[]>(
        `/api/config/header-blocklist-rules?include_disabled=${includeDisabled}`
      ),
    get: (id: number) => request<HeaderBlocklistRule>(`/api/config/header-blocklist-rules/${id}`),
    create: (data: HeaderBlocklistRuleCreate) =>
      request<HeaderBlocklistRule>("/api/config/header-blocklist-rules", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: number, data: HeaderBlocklistRuleUpdate) =>
      request<HeaderBlocklistRule>(`/api/config/header-blocklist-rules/${id}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }),
    delete: (id: number) =>
      request<void>(`/api/config/header-blocklist-rules/${id}`, {
        method: "DELETE",
      }),
  },
  userAgentClientRules: {
    list: (includeDisabled = true) =>
      request<UserAgentClientRule[]>(
        `/api/config/user-agent-client-rules?include_disabled=${includeDisabled}`
      ),
    get: (id: number) => request<UserAgentClientRule>(`/api/config/user-agent-client-rules/${id}`),
    create: (data: UserAgentClientRuleCreate) =>
      request<UserAgentClientRule>("/api/config/user-agent-client-rules", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: number, data: UserAgentClientRuleUpdate) =>
      request<UserAgentClientRule>(`/api/config/user-agent-client-rules/${id}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }),
    delete: (id: number) =>
      request<void>(`/api/config/user-agent-client-rules/${id}`, {
        method: "DELETE",
      }),
  },
};
