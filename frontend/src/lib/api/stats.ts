import { requestStats } from "./requestStats";
import { statistics } from "./statistics";

// Keep the existing stats namespace while each backend read surface owns its client.
export const stats = {
  ...statistics,
  ...requestStats,
};

export type {
  TerminalTargetStatistic,
  TerminalTargetStatisticsResponse,
} from "./statistics";
