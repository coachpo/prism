// Production container server for the built frontend. It serves static files
// from dist/ and exposes /health for probes; local development should use Vite.
import { createServer } from "node:http"
import { createReadStream, readFileSync } from "node:fs"
import { stat } from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const DIST_DIR = path.join(__dirname, "dist")
const INDEX_FILE = path.join(DIST_DIR, "index.html")
const PORT = Number(process.env.PORT ?? "3000")
const APP_VERSION = resolveAppVersion()
const HEALTH_BODY = JSON.stringify({ status: "ok", version: APP_VERSION })

const CONTENT_TYPES = new Map([
  [".css", "text/css; charset=utf-8"],
  [".html", "text/html; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".map", "application/json; charset=utf-8"],
  [".png", "image/png"],
  [".svg", "image/svg+xml"],
  [".txt", "text/plain; charset=utf-8"],
  [".woff", "font/woff"],
  [".woff2", "font/woff2"],
])

function resolveAppVersion() {
  try {
    const packageJson = JSON.parse(readFileSync(path.join(__dirname, "package.json"), "utf8"))
    return typeof packageJson?.version === "string" && packageJson.version.trim() ? packageJson.version.trim() : "0.0.0"
  } catch {
    return "0.0.0"
  }
}

function sendJson(res, statusCode, body, method = "GET") {
  const contentLength = Buffer.byteLength(body)
  res.writeHead(statusCode, {
    "Content-Length": String(contentLength),
    "Content-Type": "application/json; charset=utf-8",
  })
  if (method === "HEAD") {
    res.end()
    return
  }
  res.end(body)
}

function sendText(res, statusCode, body) {
  const contentLength = Buffer.byteLength(body)
  res.writeHead(statusCode, {
    "Content-Length": String(contentLength),
    "Content-Type": "text/plain; charset=utf-8",
  })
  res.end(body)
}

function getContentType(filePath) {
  const extension = path.extname(filePath)
  return CONTENT_TYPES.get(extension) ?? "application/octet-stream"
}

async function resolveFile(requestPath) {
  const relativePath = requestPath === "/" ? "" : requestPath.replace(/^\/+/, "")
  const filePath = path.resolve(DIST_DIR, relativePath)

  if (!filePath.startsWith(DIST_DIR + path.sep) && filePath !== DIST_DIR) {
    return null
  }

  try {
    const info = await stat(filePath)
    if (info.isFile()) {
      return filePath
    }
    if (info.isDirectory()) {
      const directoryIndex = path.join(filePath, "index.html")
      const indexInfo = await stat(directoryIndex)
      if (indexInfo.isFile()) {
        return directoryIndex
      }
    }
  } catch {
    return null
  }

  return null
}

const server = createServer(async (req, res) => {
  const method = req.method ?? "GET"

  let pathname = "/"
  try {
    const url = new URL(req.url ?? "/", "http://localhost")
    pathname = decodeURIComponent(url.pathname)
  } catch {
    sendText(res, 400, "Bad Request")
    return
  }

  if (pathname === "/health") {
    if (method === "GET" || method === "HEAD") {
      sendJson(res, 200, HEALTH_BODY, method)
      return
    }
    sendText(res, 405, "Method Not Allowed")
    return
  }

  if (method !== "GET" && method !== "HEAD") {
    sendText(res, 405, "Method Not Allowed")
    return
  }

  const resolvedPath = await resolveFile(pathname)
  const filePath = resolvedPath ?? INDEX_FILE

  try {
    const fileInfo = await stat(filePath)
    if (!fileInfo.isFile()) {
      sendText(res, 404, "Not Found")
      return
    }

    res.writeHead(200, {
      "Content-Length": String(fileInfo.size),
      "Content-Type": getContentType(filePath),
    })

    if (method === "HEAD") {
      res.end()
      return
    }

    const stream = createReadStream(filePath)
    stream.on("error", () => {
      if (!res.headersSent) {
        sendText(res, 500, "Internal Server Error")
        return
      }
      res.destroy()
    })
    stream.pipe(res)
  } catch {
    sendText(res, 500, "Internal Server Error")
  }
})

server.listen(PORT, "0.0.0.0", () => {
  process.stdout.write(`Serving dist at http://0.0.0.0:${PORT}\n`)
})
