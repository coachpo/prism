import { http, HttpResponse } from "msw"

export const rewriteMswHandlers = [
  http.get("/api/rewrite-harness/health", () =>
    HttpResponse.json({ status: "ok", source: "msw-test-harness" }),
  ),
]
