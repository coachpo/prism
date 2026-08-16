import assert from "node:assert/strict";
import { readFileSync, globSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const rootDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir });
const {
  contrastRatio,
  operatorColorTokens,
  operatorDensityModes,
  operatorDensityVariables,
  operatorStatusMarkers,
  operatorStatusTiers,
  shadcnAliasTokens,
} = load(path.join(rootDir, "src/shared/design-system/foundation.ts"));

const css = readFileSync(path.join(rootDir, "src/index.css"), "utf8");

/**
 * Executable token guard. `src/index.css` ships the values and
 * `foundation.ts` states the contract; this fails when they drift, when a
 * declared token is referenced nowhere, or when a color misses its contrast
 * floor. DESIGN.md: "Never approve a color by eye."
 */

const sources = globSync("src/**/*.{ts,tsx,css}", { cwd: rootDir }).map((file) =>
  readFileSync(path.join(rootDir, file), "utf8"),
);

function blockFor(selector) {
  const start = css.indexOf(`${selector} {`);
  assert.notEqual(start, -1, `missing ${selector} block in index.css`);
  return css.slice(start, css.indexOf("\n}", start));
}

function declarationsIn(selector) {
  const declarations = new Map();
  for (const match of blockFor(selector).matchAll(/--([a-z0-9-]+):\s*([^;]+);/g)) {
    declarations.set(match[1], match[2].trim());
  }
  return declarations;
}

const rootDeclarations = declarationsIn(":root");
const darkDeclarations = declarationsIn(".dark");
const panel = operatorColorTokens.find((token) => token.name === "panel");

test("every contracted color token is declared in :root", () => {
  const missing = operatorColorTokens
    .map((token) => token.name)
    .filter((name) => !rootDeclarations.has(name));
  assert.deepEqual(missing, []);
});

test("every token with a distinct dark value declares one", () => {
  const missing = operatorColorTokens
    .filter((token) => token.light !== token.dark)
    .map((token) => token.name)
    .filter((name) => !darkDeclarations.has(name));
  assert.deepEqual(missing, []);
});

test("index.css matches the contracted hex values exactly", () => {
  const drift = [];
  for (const token of operatorColorTokens) {
    const light = rootDeclarations.get(token.name);
    if (light && light !== token.light) drift.push(`${token.name} light ${light} != ${token.light}`);
    const dark = darkDeclarations.get(token.name);
    if (dark && dark !== token.dark) drift.push(`${token.name} dark ${dark} != ${token.dark}`);
  }
  assert.deepEqual(drift, []);
});

test("shadcn aliases stay references, never a second palette", () => {
  const literals = [];
  for (const alias of shadcnAliasTokens) {
    const value = rootDeclarations.get(alias);
    if (!value) {
      literals.push(`${alias} is not declared`);
      continue;
    }
    if (!value.startsWith("var(--")) literals.push(`${alias} = ${value}`);
  }
  assert.deepEqual(literals, []);
});

test("no color literal is declared outside the contract", () => {
  const known = new Set([
    ...operatorColorTokens.map((token) => token.name),
    ...shadcnAliasTokens,
  ]);
  const undeclared = [...rootDeclarations.entries()]
    .filter(([name, value]) => /^#[0-9a-f]{3,8}$/i.test(value) && !known.has(name))
    .map(([name]) => name);
  assert.deepEqual(undeclared, []);
});

test("every measured token clears its contrast floor in both themes", () => {
  const failures = [];
  for (const token of operatorColorTokens) {
    if (token.minContrast === null) continue;
    const light = contrastRatio(token.light, panel.light);
    const dark = contrastRatio(token.dark, panel.dark);
    if (light < token.minContrast) {
      failures.push(`${token.name} light ${light.toFixed(2)}:1 < ${token.minContrast}`);
    }
    if (dark < token.minContrast) {
      failures.push(`${token.name} dark ${dark.toFixed(2)}:1 < ${token.minContrast}`);
    }
  }
  assert.deepEqual(failures, []);
});

test("text-disabled stays decoration because it cannot clear the text floor", () => {
  const disabled = operatorColorTokens.find((token) => token.name === "text-disabled");
  assert.equal(disabled.role, "decoration");
  assert.equal(disabled.minContrast, null);
  assert.ok(contrastRatio(disabled.light, panel.light) < 4.5);
});

/**
 * Series separation is measured perceptually, not by hue angle. Hue alone
 * misjudges pairs that differ mostly in lightness or chroma — spectrum-1
 * (saturated blue) and spectrum-2 (dark teal) sit 35 degrees apart yet are
 * never confusable. CIE76 dE of 10 is roughly where two colors stop reading as
 * shades of one another, so adjacent series must clear 30 and no pair may fall
 * under 15.
 */
test("spectrum series stay perceptually separable in both themes", () => {
  const spectrum = operatorColorTokens.filter((token) => token.role === "spectrum");
  assert.equal(spectrum.length, 6);

  for (const theme of ["light", "dark"]) {
    const values = spectrum.map((token) => token[theme]);
    for (let index = 1; index < values.length; index += 1) {
      const distance = deltaE76(values[index - 1], values[index]);
      assert.ok(
        distance >= 30,
        `${theme}: spectrum-${index} and spectrum-${index + 1} are only dE ${distance.toFixed(1)} apart`,
      );
    }
    for (let a = 0; a < values.length; a += 1) {
      for (let b = a + 1; b < values.length; b += 1) {
        const distance = deltaE76(values[a], values[b]);
        assert.ok(
          distance >= 15,
          `${theme}: spectrum-${a + 1} and spectrum-${b + 1} are only dE ${distance.toFixed(1)} apart`,
        );
      }
    }
  }
});

test("no contracted token is dead", () => {
  const dead = operatorColorTokens
    .filter((token) => {
      const asVariable = `--${token.name}`;
      const asUtility = new RegExp(`[a-z](?:-[a-z0-9]+)*-${escapeRegExp(token.name)}(?:[/ "'\`]|$)`, "m");
      return !sources.some((source) => source.includes(asVariable) || asUtility.test(source));
    })
    .map((token) => token.name);
  assert.deepEqual(dead, []);
});

test("both density modes define every contracted density variable", () => {
  const standard = blockFor("html");
  const compact = blockFor('html[data-density="compact"]');
  for (const variable of Object.values(operatorDensityVariables).flat()) {
    assert.ok(standard.includes(variable), `${variable} missing from the standard density mode`);
    assert.ok(compact.includes(variable), `${variable} missing from the compact density mode`);
  }
});

test("exactly two density modes exist", () => {
  assert.deepEqual([...operatorDensityModes], ["standard", "compact"]);
});

test("every runtime tier carries a distinct shape marker", () => {
  const markers = operatorStatusTiers.map((tier) => operatorStatusMarkers[tier]);
  assert.ok(markers.every(Boolean));
  assert.equal(new Set(markers).size, markers.length);
});

test("the retired six-state palette is gone from index.css", () => {
  for (const retired of ["--success", "--warning", "--downgrade", "--info", "--unhealthy"]) {
    assert.ok(!css.includes(`${retired}:`), `${retired} should have collapsed into the four tiers`);
  }
});

test("the retired surface ladder and glow shadows are gone from index.css", () => {
  for (const retired of [
    "--surface-container-low",
    "--surface-container-high",
    "--surface-container",
    "--outline-variant",
    "--shadow-operator-glow",
    "--shadow-operator-panel",
    "--operator-glow",
  ]) {
    assert.ok(!css.includes(`${retired}:`), `${retired} should have been removed`);
  }
});

test("no product code reaches past the tokens for a raw palette color", () => {
  const rawPalette =
    /\b(?:bg|text|border|ring|fill|stroke|divide|from|to|via|outline|decoration|shadow)-(?:red|green|blue|amber|yellow|orange|emerald|slate|gray|zinc|neutral|stone|sky|indigo|violet|purple|pink|rose|teal|cyan|lime)-\d{2,3}\b/;
  const offenders = globSync("src/**/*.{ts,tsx}", { cwd: rootDir }).filter((file) =>
    rawPalette.test(readFileSync(path.join(rootDir, file), "utf8")),
  );
  assert.deepEqual(offenders, []);
});

/**
 * All visible copy goes through `messages`. This catches the common shape —
 * a Chinese string sitting directly between JSX tags — which is how hard-coded
 * labels have crept back in before.
 */
test("no visible Chinese literal is hard-coded in JSX", () => {
  const jsxChineseText = />[^<>{}\n]*[\u4e00-\u9fff][^<>{}\n]*</;
  const offenders = globSync("src/**/*.tsx", { cwd: rootDir })
    .filter((file) => !String(file).includes("i18n/"))
    .filter((file) => jsxChineseText.test(readFileSync(path.join(rootDir, String(file)), "utf8")));
  assert.deepEqual(offenders, []);
});

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function toLab(hex) {
  const value = hex.replace("#", "");
  const [r, g, b] = [0, 2, 4]
    .map((offset) => Number.parseInt(value.slice(offset, offset + 2), 16) / 255)
    .map((channel) => (channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4));
  const x = (0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047;
  const y = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  const z = (0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883;
  const f = (t) => (t > 0.008856 ? Math.cbrt(t) : 7.787 * t + 16 / 116);
  return [116 * f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))];
}

function deltaE76(left, right) {
  const a = toLab(left);
  const b = toLab(right);
  return Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);
}
