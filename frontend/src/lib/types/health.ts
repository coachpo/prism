export interface BackendHealthResponse {
  status: string;
  version: string;
  liveness: string;
  readiness: string;
  startup: string;
}
