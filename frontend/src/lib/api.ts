import { ApiError, getApiProfileId } from "./api/core";
import { auth, settings } from "./api/authSettings";
import {
  audit,
  config,
  loadbalance,
  settingsAudit,
  settingsCosting,
  settingsRetention,
  settingsTimezone,
  stats,
} from "./api/observability";
import {
  connections,
  endpoints,
  loadbalanceStrategies,
  models,
  pricingTemplates,
} from "./api/management";

export { ApiError, getApiProfileId };
export { stats } from "./api/observability";

export const api = {
  audit,
  auth,
  config,
  connections,
  endpoints,
  loadbalance,
  loadbalanceStrategies,
  models,
  pricingTemplates,
  settings: {
    ...settings,
    audit: settingsAudit,
    costing: settingsCosting,
    retention: settingsRetention,
    timezone: settingsTimezone,
  },
  stats,
};
