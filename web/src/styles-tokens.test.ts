import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const srcDir = join(import.meta.dirname);
const styles = readFileSync(join(srcDir, "styles.css"), "utf8");

// The body of the rule whose selector text is exactly `selector`, by brace matching.
function block(selector: string): string {
  const start = styles.indexOf(`${selector} {`);
  expect(start, `${selector} block`).toBeGreaterThanOrEqual(0);
  const open = styles.indexOf("{", start);
  let depth = 0;
  for (let index = open; index < styles.length; index += 1) {
    if (styles[index] === "{") depth += 1;
    if (styles[index] === "}") depth -= 1;
    if (depth === 0) return styles.slice(open + 1, index);
  }
  throw new Error(`${selector} block is not closed`);
}

function declarations(body: string): Map<string, string> {
  const tokens = new Map<string, string>();
  for (const [, name, value] of body.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) {
    tokens.set(name, value.replace(/\s+/g, " ").trim());
  }
  return tokens;
}

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return /\.(css|tsx?)$/.test(entry.name) ? [path] : [];
  });
}

describe("styles.css tokens", () => {
  it("declares the same light tokens for the light choice and the light system theme", () => {
    const chosen = declarations(block(':root[data-theme="light"]'));
    const system = declarations(block(':root:not([data-theme="dark"])'));
    expect(Object.fromEntries(system)).toEqual(Object.fromEntries(chosen));
  });

  it("uses every token it defines", () => {
    const defined = new Set(
      [...styles.matchAll(/(?:^|[{;\s])(--[\w-]+)\s*:/gm)].map(
        ([, name]) => name,
      ),
    );
    const sources = sourceFiles(srcDir)
      .map((path) => readFileSync(path, "utf8"))
      .join("\n");
    const used = new Set(
      [...sources.matchAll(/var\(\s*(--[\w-]+)/g)].map(([, name]) => name),
    );
    expect([...defined].filter((name) => !used.has(name)).sort()).toEqual([]);
  });
});
