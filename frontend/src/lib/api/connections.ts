import type {
  Connection,
  ConnectionReferencesResponse,
} from "../types";
import { request } from "./request";

export const connections = {
  list: () => request<Connection[]>("/api/connections"),
  get: (id: number) => request<Connection>(`/api/connections/${id}`),
  references: (id: number) =>
    request<ConnectionReferencesResponse>(`/api/connections/${id}/references`),
};
