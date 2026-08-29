#!/usr/bin/env node

import { spawn, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, realpathSync } from "node:fs";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";

const PLAYWRIGHT_IMAGE =
  "mcr.microsoft.com/playwright@sha256:b0ab6f3cb99aa7803adbc14d9027ec1785fc6e433b97e134e0f8fe61683b6b53";
const EXPECTED_PLAYWRIGHT_VERSION = "1.59.1";
const EXPECTED_VITE_VERSION = "8.0.16";
const DEFAULT_BASE_URL = "http://127.0.0.1:15174";
const CONTAINER_WORKSPACE = "/workspace";
const SIGNAL_EXIT_CODES = { SIGINT: 130, SIGTERM: 143, SIGHUP: 129 };

const scriptPath = fileURLToPath(import.meta.url);
const frontendRoot = path.resolve(path.dirname(scriptPath), "..");
const projectRoot = path.resolve(frontendRoot, "..");

function childOutcome(child) {
  return new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });
}

function isRunning(child) {
  return child && child.exitCode === null && child.signalCode === null;
}

function signalExitCode(signal) {
  return SIGNAL_EXIT_CODES[signal] ?? 1;
}

function delay(milliseconds) {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

async function isPortOpen(host, port) {
  return new Promise((resolve) => {
    const socket = net.createConnection({ host, port });
    const finish = (open) => {
      socket.removeAllListeners();
      socket.destroy();
      resolve(open);
    };
    socket.setTimeout(500, () => finish(false));
    socket.once("connect", () => finish(true));
    socket.once("error", () => finish(false));
  });
}

async function waitForPortClosed(host, port) {
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline) {
    if (!(await isPortOpen(host, port))) {
      return;
    }
    await delay(100);
  }
  throw new Error(`Vite port ${host}:${port} remained open after cleanup`);
}

async function waitForHealth(baseURL) {
  const deadline = Date.now() + 60_000;
  const healthURL = new URL("/health", baseURL);
  while (Date.now() < deadline) {
    try {
      const response = await fetch(healthURL, {
        signal: AbortSignal.timeout(2_000),
      });
      const body = await response.json();
      if (response.ok && body?.status === "ok") {
        return;
      }
    } catch {
      // The bounded poll below owns startup readiness.
    }
    await delay(100);
  }
  throw new Error(`Vite health check did not become ready at ${healthURL}`);
}

async function runInsideContainer(playwrightArgs) {
  const baseURL = process.env.PLAYWRIGHT_BASE_URL?.trim() || DEFAULT_BASE_URL;
  const parsedBaseURL = new URL(baseURL);
  if (
    parsedBaseURL.protocol !== "http:" ||
    !["127.0.0.1", "localhost"].includes(parsedBaseURL.hostname) ||
    parsedBaseURL.pathname !== "/" ||
    parsedBaseURL.search ||
    parsedBaseURL.hash
  ) {
    throw new Error(`closed-loop E2E requires a local HTTP origin, received ${baseURL}`);
  }

  const host = parsedBaseURL.hostname;
  const port = Number(parsedBaseURL.port || "80");
  if (!Number.isSafeInteger(port) || port <= 0 || port > 65_535) {
    throw new Error(`invalid closed-loop E2E port in ${baseURL}`);
  }
  if (await isPortOpen(host, port)) {
    throw new Error(`closed-loop E2E port is already occupied: ${host}:${port}`);
  }

  process.env.PRISM_VITE_PROXY_ENABLED = "0";
  process.env.VITE_API_BASE = "";
  process.env.PLAYWRIGHT_DISABLE_WEBSERVER = "1";
  process.env.PLAYWRIGHT_BASE_URL = baseURL;

  const [{ createServer }] = await Promise.all([import("vite")]);
  const vite = await createServer({
    root: frontendRoot,
    configFile: path.join(frontendRoot, "vite.config.ts"),
    cacheDir: path.join("/tmp", `prism-vite-cache-${process.pid}`),
    server: { host, port, strictPort: true },
  });

  let playwright = null;
  let playwrightExit = null;
  let receivedSignal = null;
  const forwardSignal = (signal) => {
    receivedSignal ??= signal;
    if (isRunning(playwright)) {
      playwright.kill(signal);
    }
  };
  for (const signal of Object.keys(SIGNAL_EXIT_CODES)) {
    process.once(signal, () => forwardSignal(signal));
  }

  let result = { code: 1, signal: null };
  let primaryError = null;
  let cleanupError = null;
  try {
    await vite.listen();
    await waitForHealth(baseURL);
    if (receivedSignal) {
      return signalExitCode(receivedSignal);
    }

    const playwrightCLI = path.join(
      frontendRoot,
      "node_modules",
      "@playwright",
      "test",
      "cli.js",
    );
    playwright = spawn(
      process.execPath,
      [playwrightCLI, "test", "--output=/tmp/playwright-output/results", ...playwrightArgs],
      {
        cwd: frontendRoot,
        env: process.env,
        stdio: "inherit",
      },
    );
    playwrightExit = childOutcome(playwright);
    result = await playwrightExit;
  } catch (error) {
    primaryError = error;
  } finally {
    try {
      if (isRunning(playwright)) {
        playwright.kill(receivedSignal || "SIGTERM");
        await Promise.race([playwrightExit, delay(5_000)]);
        if (isRunning(playwright)) {
          playwright.kill("SIGKILL");
          await playwrightExit;
        }
      }
      await vite.close();
      await waitForPortClosed(host, port);
    } catch (error) {
      cleanupError = error;
    }
  }

  if (primaryError) {
    throw primaryError;
  }
  const exitCode = receivedSignal
    ? signalExitCode(receivedSignal)
    : result.code ?? signalExitCode(result.signal);
  if (cleanupError) {
    if (exitCode !== 0) {
      console.error(`closed-loop E2E cleanup also failed: ${cleanupError.message}`);
      return exitCode;
    }
    throw cleanupError;
  }
  return exitCode;
}

function runDockerControl(args) {
  const result = spawnSync("docker", args, {
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 15_000,
  });
  if (result.error || result.status !== 0) {
    const detail = result.error?.message || result.stderr?.trim() || "unknown error";
    throw new Error(`docker ${args[0]} failed during cleanup: ${detail}`);
  }
  return result.stdout;
}

function containerExists(containerName) {
  const output = runDockerControl([
    "container",
    "ls",
    "--all",
    "--filter",
    `name=^/${containerName}$`,
    "--format",
    "{{.Names}}",
  ]);
  return output
    .split("\n")
    .map((name) => name.trim())
    .filter(Boolean)
    .includes(containerName);
}

function removeContainer(containerName) {
  if (!containerExists(containerName)) {
    return;
  }
  runDockerControl(["rm", "--force", containerName]);
  if (containerExists(containerName)) {
    throw new Error(`closed-loop E2E container still exists after cleanup: ${containerName}`);
  }
}

function readPackageVersion(packageRoot) {
  const packageJSON = JSON.parse(readFileSync(path.join(packageRoot, "package.json"), "utf8"));
  return packageJSON.version;
}

async function runOnHost(playwrightArgs) {
  if (process.platform !== "linux" || !process.getuid || !process.getgid) {
    throw new Error("closed-loop E2E container runner requires Linux UID/GID support");
  }

  const nodeModulesRoot = realpathSync(path.join(frontendRoot, "node_modules"));
  const playwrightVersion = readPackageVersion(
    path.join(nodeModulesRoot, "@playwright", "test"),
  );
  const viteVersion = readPackageVersion(path.join(nodeModulesRoot, "vite"));
  if (playwrightVersion !== EXPECTED_PLAYWRIGHT_VERSION) {
    throw new Error(
      `Playwright dependency ${playwrightVersion} does not match pinned image ${EXPECTED_PLAYWRIGHT_VERSION}`,
    );
  }
  if (viteVersion !== EXPECTED_VITE_VERSION) {
    throw new Error(
      `Vite dependency ${viteVersion} does not match closed-loop runner ${EXPECTED_VITE_VERSION}`,
    );
  }
  const artifactsRoot = path.join(frontendRoot, "artifacts");
  const testResultsRoot = path.join(frontendRoot, "test-results");
  mkdirSync(artifactsRoot, { recursive: true });
  mkdirSync(testResultsRoot, { recursive: true });

  const uid = process.getuid();
  const gid = process.getgid();
  const containerName = `prism-closed-loop-e2e-${process.pid}`;
  const containerNodeModules = path.join(CONTAINER_WORKSPACE, "frontend", "node_modules");
  const dockerArgs = [
    "run",
    "--rm",
    "--init",
    "--pull=never",
    "--name",
    containerName,
    "--network=none",
    "--read-only",
    "--shm-size=1g",
    "--user",
    `${uid}:${gid}`,
    "--env",
    "HOME=/tmp",
    "--env",
    "XDG_CACHE_HOME=/tmp/xdg-cache",
    "--env",
    "PLAYWRIGHT_BROWSERS_PATH=/ms-playwright",
    "--env",
    "PLAYWRIGHT_DISABLE_WEBSERVER=1",
    "--env",
    `PLAYWRIGHT_BASE_URL=${DEFAULT_BASE_URL}`,
    "--env",
    "PRISM_VITE_PROXY_ENABLED=0",
    "--env",
    "VITE_API_BASE=",
    "--env",
    "PRISM_CLOSED_LOOP_E2E_CONTAINER=1",
    "--tmpfs",
    `/tmp:rw,exec,nosuid,size=2g,uid=${uid},gid=${gid}`,
    "--tmpfs",
    `${containerNodeModules}/.tmp:rw,exec,nosuid,size=256m,uid=${uid},gid=${gid}`,
    "--tmpfs",
    `${containerNodeModules}/.vite:rw,exec,nosuid,size=512m,uid=${uid},gid=${gid}`,
    "--tmpfs",
    `${containerNodeModules}/.vite-temp:rw,exec,nosuid,size=256m,uid=${uid},gid=${gid}`,
    "--mount",
    `type=bind,src=${projectRoot},dst=${CONTAINER_WORKSPACE},readonly`,
    "--mount",
    `type=bind,src=${nodeModulesRoot},dst=${containerNodeModules},readonly`,
    "--mount",
    `type=bind,src=${artifactsRoot},dst=${CONTAINER_WORKSPACE}/frontend/artifacts`,
    "--mount",
    `type=bind,src=${testResultsRoot},dst=/tmp/playwright-output`,
    "--workdir",
    `${CONTAINER_WORKSPACE}/frontend`,
    PLAYWRIGHT_IMAGE,
    "timeout",
    "--signal=TERM",
    "--kill-after=15s",
    "20m",
    "node",
    "scripts/run-playwright-closed-loop.mjs",
    ...playwrightArgs,
  ];

  let docker = null;
  let receivedSignal = null;
  const forwardSignal = (signal) => {
    receivedSignal ??= signal;
    if (isRunning(docker)) {
      docker.kill(signal);
    }
  };
  for (const signal of Object.keys(SIGNAL_EXIT_CODES)) {
    process.once(signal, () => forwardSignal(signal));
  }

  let result = { code: 1, signal: null };
  let primaryError = null;
  let cleanupError = null;
  try {
    docker = spawn("docker", dockerArgs, { stdio: "inherit" });
    result = await childOutcome(docker);
  } catch (error) {
    primaryError = error;
  } finally {
    try {
      removeContainer(containerName);
    } catch (error) {
      cleanupError = error;
    }
  }

  if (primaryError) {
    throw primaryError;
  }
  const exitCode = receivedSignal
    ? signalExitCode(receivedSignal)
    : result.code ?? signalExitCode(result.signal);
  if (cleanupError) {
    if (exitCode !== 0) {
      console.error(`closed-loop container cleanup also failed: ${cleanupError.message}`);
      return exitCode;
    }
    throw cleanupError;
  }
  return exitCode;
}

const playwrightArgs = process.argv.slice(2);
try {
  const containerMarker = process.env.PRISM_CLOSED_LOOP_E2E_CONTAINER === "1";
  const expectedContainerRoot = path.join(CONTAINER_WORKSPACE, "frontend");
  const trustedContainerInvocation =
    containerMarker && frontendRoot === expectedContainerRoot && existsSync("/.dockerenv");
  if (containerMarker && !trustedContainerInvocation) {
    throw new Error("PRISM_CLOSED_LOOP_E2E_CONTAINER is reserved for the isolated runner");
  }
  const exitCode = trustedContainerInvocation
    ? await runInsideContainer(playwrightArgs)
    : await runOnHost(playwrightArgs);
  process.exitCode = exitCode;
} catch (error) {
  console.error(`closed-loop E2E runner failed: ${error.message}`);
  process.exitCode = 1;
}
