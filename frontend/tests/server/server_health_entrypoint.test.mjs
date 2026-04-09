import { after, before, test } from "node:test"
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { readFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const frontendDir = path.resolve(__dirname, "../..")
const packageJson = JSON.parse(readFileSync(path.join(frontendDir, "package.json"), "utf8"))
const port = 33000 + Math.floor(Math.random() * 1000)

let serverProcess

function waitForServerReady(childProcess) {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      reject(new Error("Timed out waiting for frontend server to start"))
    }, 10_000)

    const handleStdout = (chunk) => {
      const text = chunk.toString()
      if (text.includes(`Serving dist at http://0.0.0.0:${port}`)) {
        clearTimeout(timeout)
        childProcess.stdout.off("data", handleStdout)
        resolve()
      }
    }

    childProcess.once("error", (error) => {
      clearTimeout(timeout)
      reject(error)
    })
    childProcess.once("exit", (code) => {
      clearTimeout(timeout)
      reject(new Error(`Frontend server exited before ready with code ${code}`))
    })
    childProcess.stdout.on("data", handleStdout)
  })
}

before(async () => {
  serverProcess = spawn("node", ["server.mjs"], {
    cwd: frontendDir,
    env: {
      ...process.env,
      PORT: String(port),
    },
    stdio: ["ignore", "pipe", "pipe"],
  })

  await waitForServerReady(serverProcess)
})

after(async () => {
  if (!serverProcess || serverProcess.exitCode !== null) {
    return
  }

  await new Promise((resolve) => {
    serverProcess.once("exit", resolve)
    serverProcess.kill("SIGTERM")
  })
})

test("frontend server /health GET returns liveness payload", async () => {
  const response = await fetch(`http://127.0.0.1:${port}/health`)

  assert.equal(response.status, 200)
  assert.equal(response.headers.get("content-type"), "application/json; charset=utf-8")
  assert.deepEqual(await response.json(), {
    status: "ok",
    version: packageJson.version,
  })
})

test("frontend server /health HEAD preserves status and omits body", async () => {
  const response = await fetch(`http://127.0.0.1:${port}/health`, {
    method: "HEAD",
  })

  assert.equal(response.status, 200)
  assert.equal(response.headers.get("content-type"), "application/json; charset=utf-8")
  assert.equal(await response.text(), "")
})

test("frontend server /health rejects unsupported methods", async () => {
  const response = await fetch(`http://127.0.0.1:${port}/health`, {
    method: "POST",
  })

  assert.equal(response.status, 405)
  assert.equal(response.headers.get("content-type"), "text/plain; charset=utf-8")
  assert.equal(await response.text(), "Method Not Allowed")
})
