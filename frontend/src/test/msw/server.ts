import { setupServer } from "msw/node"
import { rewriteMswHandlers } from "./handlers"

export const rewriteTestServer = setupServer(...rewriteMswHandlers)
