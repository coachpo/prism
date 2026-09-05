import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const rootDir = path.resolve(__dirname, "../..");
const messagesFile = path.join(rootDir, "src/i18n/messages/zh-CN.ts");
const messages = readFileSync(messagesFile, "utf8");

/**
 * Executable copy and overlay guards.
 *
 * 这些缺陷都回流过：全角开括号配半角闭括号、两套中文引号、对话框宽度绕开档位。
 * 它们靠人眼复核抓不住，所以写成断言。
 */

const CJK = "\\u4e00-\\u9fff\\u3400-\\u4dbf";

function messageLines() {
  return messages.split("\n").map((line, index) => ({ line, no: index + 1 }));
}

test("Chinese copy uses one quote style", () => {
  const offenders = messageLines()
    .filter(({ line }) => /[“”]/.test(line))
    .map(({ no, line }) => `${no}: ${line.trim()}`);
  assert.deepEqual(
    offenders,
    [],
    "中文引号统一为「」，不要混用弯引号",
  );
});

test("full-width parentheses stay paired", () => {
  const offenders = messageLines()
    .filter(({ line }) => {
      const open = (line.match(/（/g) ?? []).length;
      const close = (line.match(/）/g) ?? []).length;
      return open !== close;
    })
    .map(({ no, line }) => `${no}: ${line.trim()}`);
  assert.deepEqual(offenders, [], "全角括号必须配对，不能配半角");
});

test("Chinese runs do not carry half-width commas or colons", () => {
  // 数学区间记法（`[开始, 结束)`）是唯一例外：那里的逗号属于记法本身。
  const offenders = messageLines()
    .filter(({ line }) => !line.includes("半开区间"))
    .filter(({ line }) => new RegExp(`[${CJK}][,:](?![/\\d])`).test(line))
    .map(({ no, line }) => `${no}: ${line.trim()}`);
  assert.deepEqual(offenders, [], "中文正文里用「，」「：」，不用半角标点");
});

test("cost copy does not fork into 花费 / 费用 / 支出 for known cost", () => {
  // 「已知成本」是术语表里的词。同一个量再叫「花费」「费用」，操作者要自己
  // 折算这三个词说的是不是一件事。
  const offenders = messageLines()
    .filter(({ line }) => /已知(花费|费用|支出)/.test(line))
    .map(({ no, line }) => `${no}: ${line.trim()}`);
  assert.deepEqual(offenders, [], "已知成本只叫「已知成本」");
});

function walkTsx(dir, files = []) {
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) {
      walkTsx(full, files);
    } else if (entry.endsWith(".tsx")) {
      files.push(full);
    }
  }
  return files;
}

test("dialog and sheet widths go through the size prop, never max-w-*", () => {
  const primitives = new Set([
    path.join(rootDir, "src/components/ui/dialog.tsx"),
    path.join(rootDir, "src/components/ui/sheet.tsx"),
    path.join(rootDir, "src/components/ui/alert-dialog.tsx"),
  ]);
  const offenders = [];
  for (const file of walkTsx(path.join(rootDir, "src"))) {
    if (primitives.has(file)) continue;
    const source = readFileSync(file, "utf8");
    for (const match of source.matchAll(
      /<(DialogContent|SheetContent)\b[^>]*?>/gs,
    )) {
      if (/\bmax-w-/.test(match[0])) {
        offenders.push(`${path.relative(rootDir, file)}: ${match[1]}`);
      }
    }
  }
  assert.deepEqual(
    offenders,
    [],
    "对话框与抽屉宽度只走 size 档位（420/560/720），调用处不写 max-w-*",
  );
});
