import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import path from "node:path"
import test from "node:test"
import { fileURLToPath } from "node:url"

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)
const source = readFileSync(path.resolve(__dirname, "../../src/main.tsx"), "utf8")

test("frontend main entrypoint keeps the reviewed provider bootstrap order", () => {
  assert.match(source, /createRoot\(document\.getElementById\("root"\)!\)\.render\(/)
  assert.match(source, /<StrictMode>/)
  assert.match(source, /<LocaleProvider>/)
  assert.match(source, /<ThemeProvider attribute="class" defaultTheme="system" enableSystem=\{true\}>/)
  assert.match(source, /<TooltipProvider>/)
  assert.match(source, /<App \/>/)
  assert.match(source, /<Toaster \/>/)

  const orderedSegments = [
    "<StrictMode>",
    "<LocaleProvider>",
    '<ThemeProvider attribute="class" defaultTheme="system" enableSystem={true}>',
    "<TooltipProvider>",
    "<App />",
    "<Toaster />",
    "</TooltipProvider>",
    "</ThemeProvider>",
    "</LocaleProvider>",
    "</StrictMode>",
  ]

  let previousIndex = -1
  for (const segment of orderedSegments) {
    const index = source.indexOf(segment)
    assert.ok(index > previousIndex, `expected ${segment} to appear after the previous provider boundary`)
    previousIndex = index
  }
})
