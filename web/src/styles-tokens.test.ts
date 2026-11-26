import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { phoneGridMaxWidth } from "./layout/justified";
import {
  compactHeaderMaxWidth,
  phoneViewportMaxWidth,
  smallPhoneMaxWidth,
  wideReaderMinWidth,
} from "./viewport";

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

// Each viewport-width @media block, by brace matching, with comments removed.
function widthQueries(): { query: string; body: string }[] {
  const css = styles.replace(/\/\*[\s\S]*?\*\//g, "");
  const queries: { query: string; body: string }[] = [];
  for (const match of css.matchAll(
    /@media([^{]*\((?:max|min)-width[^{]*)\{/g,
  )) {
    const open = (match.index ?? 0) + match[0].length;
    let depth = 1;
    let index = open;
    for (; depth > 0; index += 1) {
      if (css[index] === "{") depth += 1;
      if (css[index] === "}") depth -= 1;
    }
    queries.push({ query: match[1].trim(), body: css.slice(open, index - 1) });
  }
  return queries;
}

// The flat rules of a block body as [selector, declarations].
function rules(body: string): [string, string][] {
  return [...body.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map(
    ([, selector, declarations]) => [selector.trim(), declarations],
  );
}

function typeScale(declarations: string): Record<string, string> {
  return Object.fromEntries(
    [...declarations.matchAll(/(--type-[\w-]+)\s*:\s*([^;]+);/g)].map(
      ([, name, value]) => [name, value.trim()],
    ),
  );
}

// Each width bound in `text`, as "max-width: 723px".
function widthBounds(text: string): string[] {
  return [...text.matchAll(/\(((?:max|min)-width):\s*(\d+)px\)/g)].map(
    ([, bound, px]) => `${bound}: ${px}px`,
  );
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

// A phone grid is set by the grid's own width (#85), so grid cell CSS keys on
// .mobile-refined-grid. A viewport query styling a cell switches at another
// edge than the layout does, as #84 and #85 did.
describe("styles.css grid edge", () => {
  const gridCell =
    /\.(?:grid-row|grid-cell|cell-[\w-]+|reddit-cell|story-[\w-]+|signal-mobile-chip|kept-marker|refined-grid|mobile-refined-grid|end-of-feed|scope-cell)\b/;
  // Search results and the Related panel share cell classes and follow the
  // viewport; the finish card's narrow layout is a known viewport rule.
  const viewportScoped = /\.(?:search-results|related-panel)\b/;
  const allowed = new Set([
    "(max-width: 520px) .end-of-feed.finish-card > div",
    "(max-width: 520px) .end-of-feed.finish-card button",
  ]);

  it("keys grid cell rules on the phone grid class, not a viewport width", () => {
    const viewportCellRules = widthQueries().flatMap(({ query, body }) =>
      rules(body).flatMap(([selector]) =>
        selector
          .split(",")
          .map((part) => part.trim())
          .filter((part) => gridCell.test(part) && !viewportScoped.test(part))
          .map((part) => `${query} ${part}`),
      ),
    );
    expect(viewportCellRules.filter((rule) => !allowed.has(rule))).toEqual([]);
  });

  it("gives a phone grid the phone type scale of narrow viewports", () => {
    const viewportScale = widthQueries()
      .filter(
        ({ query }) => query === `(max-width: ${phoneViewportMaxWidth - 1}px)`,
      )
      .flatMap(({ body }) => rules(body))
      .filter(([selector]) => selector === ":root")
      .map(([, declarations]) => typeScale(declarations));
    expect(viewportScale).toHaveLength(1);
    expect(typeScale(block(".mobile-refined-grid"))).toEqual(viewportScale[0]);
  });
});

// The viewport's phone edge is one constant, so tests can take their widths
// from it. A query a pixel off it switches part of the page at another width,
// as the tag chips' and Feed triage's `max-width: 620px` did (#87).
describe("styles.css viewport phone edge", () => {
  // One phone edge for the chrome and the grid (#86): a viewport narrower
  // than the grid's edge gets the phone chrome, so a phone grid never sits
  // under desktop chrome on a device without a classic scrollbar.
  it("puts the viewport phone edge at the grid's phone edge", () => {
    expect(phoneViewportMaxWidth).toBe(phoneGridMaxWidth);
  });

  // Both bounds of each edge viewport.ts names: the phone edge, and the
  // steps inside a range (the small-phone header, the header's compaction,
  // the wide reader). The finish card's stacked layout is a CSS-only step.
  const bounds = new Set([
    ...[
      phoneViewportMaxWidth,
      smallPhoneMaxWidth,
      compactHeaderMaxWidth,
      wideReaderMinWidth,
    ].flatMap((width) => [
      `max-width: ${width - 1}px`,
      `min-width: ${width}px`,
    ]),
    "max-width: 520px",
  ]);

  it("switches at the viewport phone edge or a named step", () => {
    const stray = widthQueries().flatMap(({ query }) =>
      widthBounds(query)
        .filter((width) => !bounds.has(width))
        .map(() => query),
    );
    expect([...new Set(stray)]).toEqual([]);
  });

  // TypeScript's media queries and sizes= hints build their widths from
  // viewport.ts, so a literal width here is a stray edge, as RelatedPanel's
  // and ConfirmRemove's 700px were (#86).
  it("builds TypeScript's media queries from viewport.ts", () => {
    const literal = sourceFiles(srcDir)
      .filter((path) => /\.tsx?$/.test(path) && !/\.test\.tsx?$/.test(path))
      .flatMap((path) =>
        widthBounds(readFileSync(path, "utf8")).map(
          (width) => `${path.slice(srcDir.length + 1)} (${width})`,
        ),
      );
    expect(literal).toEqual([]);
  });
});
