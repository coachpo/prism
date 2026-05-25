import { execSync } from "node:child_process"
import { readFileSync } from "node:fs"
import path from "path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig, loadEnv, type ProxyOptions } from "vite"

const APP_VERSION = resolveAppVersion()
const HEALTH_RESPONSE = JSON.stringify({
  status: "ok",
  version: APP_VERSION,
  liveness: "ok",
  readiness: "ready",
  startup: "complete",
})
const GIT_REVISION = resolveGitRevision()
const GIT_RUN_NUMBER = resolveGitRunNumber()

function resolveAppVersion() {
  try {
    const packageJson = JSON.parse(readFileSync(path.resolve(__dirname, "package.json"), "utf8")) as {
      version?: string
    }
    return packageJson.version?.trim() || "0.0.0"
  } catch {
    return "0.0.0"
  }
}

function resolveGitRevision() {
  const revisionFromEnv =
    process.env.VITE_GIT_REVISION?.trim() || process.env.GIT_REVISION?.trim()
  if (revisionFromEnv) {
    return revisionFromEnv
  }

  try {
    return execSync("git rev-parse --short HEAD", {
      cwd: __dirname,
      stdio: ["ignore", "pipe", "ignore"],
    })
      .toString()
      .trim()
  } catch {
    return "unknown"
  }
}

function resolveGitRunNumber() {
  const runNumberFromEnv =
    process.env.VITE_GIT_RUN_NUMBER?.trim() ||
    process.env.GIT_RUN_NUMBER?.trim() ||
    process.env.GITHUB_RUN_NUMBER?.trim()
  return runNumberFromEnv || "local"
}

function createHealthMiddleware(enabled = true) {
  return (req: { method?: string; url?: string }, res: {
    statusCode: number
    setHeader: (name: string, value: string) => void
    end: (chunk: string) => void
  }, next: () => void) => {
    const pathname = req.url?.split("?", 1)[0]

    if (enabled && req.method === "GET" && pathname === "/health") {
      res.statusCode = 200
      res.setHeader("Content-Type", "application/json; charset=utf-8")
      res.end(HEALTH_RESPONSE)
      return
    }

    next()
  }
}

function isTruthyEnvFlag(value: string | undefined) {
  return value === "1" || value?.toLowerCase() === "true"
}

function createLauncherProxyConfig(target: string): Record<string, string | ProxyOptions> {
  return {
    "/api/realtime/ws": {
      target,
      changeOrigin: true,
      ws: true,
    },
    "/api": {
      target,
      changeOrigin: true,
    },
    "/health": {
      target,
      changeOrigin: true,
    },
    "/v1": {
      target,
      changeOrigin: true,
    },
    "/v1beta": {
      target,
      changeOrigin: true,
    },
  }
}

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, __dirname, "")
  const launcherProxyEnabled = isTruthyEnvFlag(env.PRISM_VITE_PROXY_ENABLED)
  const launcherProxyTarget = env.PRISM_VITE_PROXY_TARGET?.trim() || "http://localhost:8000"
  const browserApiBase = launcherProxyEnabled ? "" : env.VITE_API_BASE?.trim() || ""

  return {
    define: {
      "import.meta.env.VITE_API_BASE": JSON.stringify(browserApiBase),
      "import.meta.env.VITE_APP_VERSION": JSON.stringify(APP_VERSION),
      "import.meta.env.VITE_GIT_RUN_NUMBER": JSON.stringify(GIT_RUN_NUMBER),
      "import.meta.env.VITE_GIT_REVISION": JSON.stringify(GIT_REVISION),
    },
    plugins: [
      react(),
      tailwindcss(),
      {
        name: "health-endpoint",
        configureServer(server) {
          server.middlewares.use(createHealthMiddleware(!launcherProxyEnabled))
        },
        configurePreviewServer(server) {
          server.middlewares.use(createHealthMiddleware())
        },
      },
    ],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    server: launcherProxyEnabled
      ? {
          proxy: createLauncherProxyConfig(launcherProxyTarget),
        }
      : undefined,
  }
})
