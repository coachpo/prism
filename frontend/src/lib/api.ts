import { ApiError, getApiProfileId, setApiProfileId } from "./api/core";
import { auth, settings } from "./api/authSettings";
import {
  audit,
  config,
  health,
  loadbalance,
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
  profiles,
  vendors,
} from "./api/management";

export { ApiError, getApiProfileId, setApiProfileId };
export { health, stats } from "./api/observability";

export const api = {
  audit,
  auth,
  config,
  connections,
  health,
  endpoints,
  loadbalance,
  loadbalanceStrategies,
  models,
  pricingTemplates,
  profiles,
  vendors,
  settings: {
    ...settings,
    costing: settingsCosting,
    retention: settingsRetention,
    timezone: settingsTimezone,
  },
  stats,
};
