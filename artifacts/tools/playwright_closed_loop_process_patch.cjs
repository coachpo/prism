"use strict";

// Playwright normally creates a process group for every browser and web
// server. The closed-loop kernel already owns an isolated group, so keep those
// descendants inside it and retain Playwright's tree-wide shutdown semantics.
if (process.env.CLOSED_LOOP_CASE_ID) {
  const childProcess = require("node:child_process");
  const originalSpawn = childProcess.spawn;
  const originalKill = process.kill.bind(process);
  const containedProcessIds = new Set();

  function containedDescendants(rootPid) {
    const processTable = childProcess.spawnSync(
      "/bin/ps",
      ["-axo", "pid=,ppid="],
      { encoding: "utf8" },
    );
    if (processTable.status !== 0 || typeof processTable.stdout !== "string") {
      return [];
    }

    const childrenByParent = new Map();
    for (const line of processTable.stdout.split("\n")) {
      const [pidText, parentPidText] = line.trim().split(/\s+/, 2);
      const pid = Number(pidText);
      const parentPid = Number(parentPidText);
      if (!Number.isInteger(pid) || !Number.isInteger(parentPid)) continue;
      const children = childrenByParent.get(parentPid) || [];
      children.push(pid);
      childrenByParent.set(parentPid, children);
    }

    const descendants = [];
    const visit = (pid) => {
      for (const childPid of childrenByParent.get(pid) || []) visit(childPid);
      if (pid !== rootPid) descendants.push(pid);
    };
    visit(rootPid);
    return descendants;
  }

  function signalContainedTree(rootPid, signal) {
    let signaled = false;
    let firstError;
    for (const pid of [...containedDescendants(rootPid), rootPid]) {
      try {
        originalKill(pid, signal);
        signaled = true;
      } catch (error) {
        if (error?.code !== "ESRCH" && firstError === undefined) firstError = error;
      }
    }
    if (firstError) throw firstError;
    if (!signaled) return originalKill(rootPid, signal);
    return true;
  }

  childProcess.spawn = function containedSpawn(command, args, options) {
    let spawnArgs = args;
    let spawnOptions = options;
    if (!Array.isArray(args) && args && typeof args === "object") {
      spawnArgs = undefined;
      spawnOptions = args;
    }
    if (!spawnOptions || spawnOptions.detached !== true) {
      return originalSpawn.apply(this, arguments);
    }

    const containedOptions = { ...spawnOptions, detached: false };
    const child = spawnArgs === undefined
      ? originalSpawn.call(this, command, containedOptions)
      : originalSpawn.call(this, command, spawnArgs, containedOptions);
    if (child.pid) {
      containedProcessIds.add(child.pid);
      child.once("close", () => containedProcessIds.delete(child.pid));
    }
    return child;
  };

  process.kill = function containedKill(pid, signal) {
    if (typeof pid === "number" && pid < 0 && containedProcessIds.has(-pid)) {
      return signalContainedTree(-pid, signal);
    }
    return originalKill(pid, signal);
  };
}
