import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { runInThisContext } from "node:vm";

import ts from "typescript";

const requireFromHere = createRequire(import.meta.url);

function resolveExistingFile(basePath) {
  const candidates = [
    basePath,
    `${basePath}.ts`,
    `${basePath}.tsx`,
    `${basePath}.js`,
    path.join(basePath, "index.ts"),
    path.join(basePath, "index.tsx"),
    path.join(basePath, "index.js"),
  ];

  const resolvedPath = candidates.find((candidate) => existsSync(candidate));
  assert.ok(resolvedPath, `Could not resolve TypeScript module for ${basePath}`);
  return resolvedPath;
}

export function createTsModuleLoader({ rootDir, mocks = {} }) {
  const cache = new Map();

  function resolveProjectSpecifier(fromFilePath, specifier) {
    if (specifier.startsWith("@/")) {
      return resolveExistingFile(path.join(rootDir, "src", specifier.slice(2)));
    }

    return resolveExistingFile(path.resolve(path.dirname(fromFilePath), specifier));
  }

  function load(filePath) {
    const resolvedFilePath = resolveExistingFile(filePath);
    if (cache.has(resolvedFilePath)) {
      return cache.get(resolvedFilePath);
    }

    const source = readFileSync(resolvedFilePath, "utf8").replaceAll("import.meta.env", "({})");
    const module = { exports: {} };
    cache.set(resolvedFilePath, module.exports);

    const { outputText } = ts.transpileModule(source, {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
        jsx: ts.JsxEmit.ReactJSX,
        esModuleInterop: true,
        allowSyntheticDefaultImports: true,
      },
      fileName: resolvedFilePath,
    });

    function localRequire(specifier) {
      if (Object.prototype.hasOwnProperty.call(mocks, specifier)) {
        return mocks[specifier];
      }

      if (specifier.startsWith("@/") || specifier.startsWith(".")) {
        return load(resolveProjectSpecifier(resolvedFilePath, specifier));
      }

      return requireFromHere(specifier);
    }

    const wrapped = runInThisContext(
      `(function (exports, require, module, __filename, __dirname) {${outputText}\n})`,
      { filename: resolvedFilePath },
    );
    wrapped(
      module.exports,
      localRequire,
      module,
      resolvedFilePath,
      path.dirname(resolvedFilePath),
    );
    cache.set(resolvedFilePath, module.exports);
    return module.exports;
  }

  return { load };
}
