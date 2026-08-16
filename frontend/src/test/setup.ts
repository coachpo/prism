import { afterAll, afterEach, beforeAll } from "vitest"
import "@testing-library/jest-dom/vitest"
import { rewriteTestServer } from "./msw/server"

beforeAll(() => {
  rewriteTestServer.listen({ onUnhandledRequest: "error" })
})

afterEach(() => {
  rewriteTestServer.resetHandlers()
})

afterAll(() => {
  rewriteTestServer.close()
})
