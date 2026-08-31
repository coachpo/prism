import { ApiError, getApiProfileId } from "./api/request";
import { auth, settings } from "./api/authSettings";
import { audit } from "./api/audit";
import { config } from "./api/configRules";
import { connections } from "./api/connections";
import { endpoints } from "./api/endpoints";
import { loadbalance } from "./api/loadbalance";
import { loadbalanceStrategies } from "./api/loadbalanceStrategies";
import { models } from "./api/models";
import { pricingTemplates } from "./api/pricingTemplates";
import { stats } from "./api/stats";
import { settingsAudit } from "./api/settingsAudit";
import { settingsCosting } from "./api/settingsCosting";
import { settingsRetention } from "./api/settingsRetention";
import * as modelExportClient from "./api/modelExport";

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
  // Pi export/binding client: every Pi wire type lives in
  // `lib/types/model-export`, so `lib` never imports from `features/`.
  modelExport: modelExportClient,
  pricingTemplates,
  settings: {
    ...settings,
    audit: settingsAudit,
    costing: settingsCosting,
    retention: settingsRetention,
  },
  stats,
};
