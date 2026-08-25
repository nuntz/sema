import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify, largeRunWeight, spanEligible } from "./justified";

const item = (
  id: string | number,
  size: Item["size"],
  ratio = 1.5,
  media = true,
): Item => ({
  item_id: String(id),
  feed_id: "f",
  url: "https://example.com",
  title: `Item ${id}`,
  summary_source: "",
  published_ts: "2026-08-24T00:00:00Z",
  fetched_ts: "2026-08-24T00:00:00Z",
  has_body: true,
  extract_quality: 1,
  score: 0.5,
  size,
  read: false,
  signal: 0,
  hearted: false,
  media_url: media ? `https://example.com/${id}.jpg` : undefined,
  media_w: media ? ratio * 100 : undefined,
  media_h: media ? 100 : undefined,
});

const ids = (rows: ReturnType<typeof justify>) =>
  rows.flatMap((row) => row.cells.map((cell) => cell.item.item_id));

const fillRatio = (row: ReturnType<typeof justify>[number], width: number) =>
  (row.cells.reduce((sum, cell) => sum + cell.width, 0) +
    row.gap * Math.max(0, row.cells.length - 1)) /
  width;

const cellsAt = (row: ReturnType<typeof justify>[number], y: number) =>
  row.cells
    .filter((cell) => {
      const top = cell.offsetY ?? 0;
      return top <= y && top + (cell.height ?? row.height) >= y;
    })
    .sort((left, right) => left.left - right.left);

const expectLineFilled = (
  row: ReturnType<typeof justify>[number],
  y: number,
  width: number,
) => {
  const cells = cellsAt(row, y);
  const first = cells[0];
  const last = cells.at(-1);
  if (!first || !last) throw new Error("expected a populated visual line");
  expect(first.left).toBeCloseTo(0, 5);
  expect(last.left + last.width).toBeCloseTo(width, 5);
};

const expectNoOverlaps = (row: ReturnType<typeof justify>[number]) => {
  for (let leftIndex = 0; leftIndex < row.cells.length; leftIndex++) {
    const left = row.cells[leftIndex];
    const leftTop = left.offsetY ?? 0;
    const leftBottom = leftTop + (left.height ?? row.height);
    for (
      let rightIndex = leftIndex + 1;
      rightIndex < row.cells.length;
      rightIndex++
    ) {
      const right = row.cells[rightIndex];
      const rightTop = right.offsetY ?? 0;
      const rightBottom = rightTop + (right.height ?? row.height);
      const horizontal =
        left.left < right.left + right.width &&
        right.left < left.left + left.width;
      const vertical = leftTop < rightBottom && rightTop < leftBottom;
      expect(horizontal && vertical).toBe(false);
    }
  }
};

describe("17c desktop bands", () => {
  it("matches the handoff span threshold and 354px geometry", () => {
    expect(spanEligible(item("boundary", "L", 1.2))).toBe(true);
    expect(spanEligible(item("wide", "L", 1.201))).toBe(false);
    expect(spanEligible(item("text", "L", 1, false))).toBe(false);

    const width = 1248;
    const companions = Array.from({ length: 5 }, (_, index) =>
      item(`c${index}`, index < 2 ? "M" : "S"),
    );
    const row = justify(
      [item("hero", "L", 1.2), ...companions, item("next", "L", 1.8)],
      width,
    )[0];
    const hero = row.cells[0];
    const subHeight = row.cells[1]?.height ?? 0;

    expect(row.kind).toBe("span");
    expect(row.cells).toHaveLength(6);
    expect(row.height).toBe(354);
    expect(subHeight).toBe(172);
    expect(hero.span).toBe(2);
    expect(hero.width).toBeCloseTo(((width - 10) * 1.1) / 3.6, 5);
    expectNoOverlaps(row);
    expectLineFilled(row, subHeight / 2, width);
    expectLineFilled(row, subHeight + row.gap + subHeight / 2, width);
  });

  it.each([
    ["single M", [item("lead-m", "M", 1)]],
    ["single S", [item("lead-s", "S", 1)]],
    ["pair", [item("lead-m", "M", 1), item("lead-s", "S", 1)]],
  ])("absorbs a %s before L without reordering", (_, leading) => {
    const needed = 3 - leading.length;
    const following = Array.from({ length: needed }, (_, index) =>
      item(`after-${index}`, index % 2 === 0 ? "M" : "S", 1),
    );
    const hero = item("hero", "L", 0.8);
    const entries = [
      ...leading,
      hero,
      ...following,
      item("terminal", "L", 1.8),
    ];
    const row = justify(entries, 1248)[0];

    expect(row.kind).toBe("tall");
    expect(row.height).toBe(288);
    expect(row.cells.map((cell) => cell.item.item_id)).toEqual(
      [...leading, hero, ...following].map((entry) => entry.item_id),
    );
    expect(row.cells.find((cell) => cell.item.item_id === "hero")?.tall).toBe(
      true,
    );
    expect(fillRatio(row, 1248)).toBeCloseTo(1, 5);
  });

  it("uses 3–5-cell standards and never applies final widths inside the feed", () => {
    const run = Array.from({ length: 11 }, (_, index) =>
      item(`run-${index}`, index % 3 === 0 ? "M" : "S", 1),
    );
    const entries = [
      ...run,
      item("portrait", "L", 1),
      item("after-a", "M", 1),
      item("after-b", "S", 1),
      item("terminal", "L", 1.8),
    ];
    const rows = justify(entries, 1248);
    const standards = rows.filter((row) => row.kind === "standard");

    expect(standards.map((row) => row.cells.length)).toEqual([5, 5]);
    for (const row of standards)
      expect(fillRatio(row, 1248)).toBeGreaterThanOrEqual(0.98);
    expect(ids(rows)).toEqual(entries.map((entry) => entry.item_id));
  });

  it("fills every non-degenerate band in representative feeds", () => {
    const cases = [
      [
        item("a-hero", "L", 0.8),
        ...Array.from({ length: 6 }, (_, index) =>
          item(`a-${index}`, index % 2 === 0 ? "M" : "S"),
        ),
        item("a-straggler", "M"),
        item("a-wide", "L", 1.8),
        ...Array.from({ length: 4 }, (_, index) =>
          item(`a-tail-${index}`, "S"),
        ),
        item("a-next", "L", 1),
        ...Array.from({ length: 5 }, (_, index) =>
          item(`a-last-${index}`, "M"),
        ),
      ],
      [
        item("b-0", "L", 0.8),
        item("b-1", "L", 0.9),
        item("b-2", "L", 1.8),
        ...Array.from({ length: 7 }, (_, index) =>
          item(`b-tail-${index}`, index === 0 ? "M" : "S"),
        ),
      ],
      [
        item("c-wide", "L", 1.8),
        item("c-m", "M"),
        ...Array.from({ length: 7 }, (_, index) => item(`c-s-${index}`, "S")),
      ],
    ];

    let unsanctioned = 0;
    for (const entries of cases) {
      const rows = justify(entries, 1248);
      rows.forEach((row, index) => {
        const final = index === rows.length - 1;
        const shortSpan = row.kind === "span" && row.cells.length < 6;
        const compactCap = row.kind === "compact";
        const sanctioned = final || shortSpan || compactCap;
        if (fillRatio(row, 1248) < 0.9 && !sanctioned) unsanctioned++;
        if (!sanctioned)
          expect(fillRatio(row, 1248)).toBeGreaterThanOrEqual(0.98);
      });
    }
    expect(unsanctioned).toBe(0);
  });

  it("keeps all sanctioned underfill natural and left-aligned", () => {
    const width = 1248;
    const shortSpan = justify(
      [
        item("short-hero", "L", 1),
        ...Array.from({ length: 4 }, (_, index) =>
          item(`short-${index}`, "S", 1),
        ),
      ],
      width,
    )[0];
    expect(shortSpan.kind).toBe("span");
    expect(shortSpan.cells.slice(1).map((cell) => cell.width)).toEqual([
      120, 120, 120, 120,
    ]);
    expect(fillRatio(shortSpan, width)).toBeLessThan(0.9);

    const final = justify(
      [item("final-m", "M", 1), item("final-s", "S", 1)],
      width,
    )[0];
    expect(final.cells[0].left).toBe(0);
    expect(final.cells[0].width).toBeCloseTo(157.5, 5);
    expect(final.cells[1].width).toBeCloseTo(120, 5);
    expect(fillRatio(final, width)).toBeLessThan(0.9);

    const loneLargeRows = justify(
      [
        item("prefix-0", "M", 1),
        item("prefix-1", "S", 1),
        item("prefix-2", "M", 1),
        item("lone", "L", 1),
      ],
      width,
    );
    const loneLarge = loneLargeRows.at(-1);
    expect(loneLarge?.kind).toBe("tall");
    expect(loneLarge?.cells[0].left).toBe(0);
    expect(loneLarge?.cells[0].width).toBeCloseTo(262.5, 5);

    const compactOverflow = justify(
      Array.from({ length: 7 }, (_, index) => item(`compact-${index}`, "S", 1)),
      width,
    );
    expect(compactOverflow[0].kind).toBe("compact");
    expect(compactOverflow[0].height).toBe(132);
    expect(compactOverflow[0].cells).toHaveLength(6);
    expect(compactOverflow[0].cells.every((cell) => cell.width === 132)).toBe(
      true,
    );
    expect(compactOverflow[1].cells).toHaveLength(1);
    expect(compactOverflow[1].cells[0].left).toBe(0);
    expect(compactOverflow[1].height).toBe(132);
  });

  it("keeps one L per mosaic band, prevents adjacent spans, and alternates span sides", () => {
    const first = [
      item("first", "L", 1),
      ...Array.from({ length: 5 }, (_, index) => item(`first-${index}`, "M")),
    ];
    const second = [
      item("second", "L", 1),
      ...Array.from({ length: 8 }, (_, index) => item(`second-${index}`, "S")),
    ];
    const third = [
      item("third", "L", 1),
      ...Array.from({ length: 5 }, (_, index) => item(`third-${index}`, "M")),
    ];
    const entries = [...first, ...second, ...third];
    const rows = justify(entries, 1248);
    const spans = rows.filter((row) => row.kind === "span");

    expect(rows.map((row) => row.kind)).toEqual([
      "span",
      "tall",
      "compact",
      "span",
    ]);
    expect(spans).toHaveLength(2);
    expect(spans[0].cells[0].left).toBe(0);
    expect(spans[1].cells[0].left).toBeGreaterThan(1248 / 2);
    expect(
      rows.every(
        (row) => row.cells.filter((cell) => cell.item.size === "L").length <= 1,
      ),
    ).toBe(true);
    expect(ids(rows)).toEqual(entries.map((entry) => entry.item_id));

    const consecutive = justify(
      [item("l0", "L", 1), item("l1", "L", 1), item("l2", "L", 1)],
      1248,
    );
    expect(consecutive.map((row) => [row.kind, row.cells.length])).toEqual([
      ["hero", 3],
    ]);
  });

  it("uses 288px tall bands with the L at 1.75 basis", () => {
    const row = justify(
      [
        item("wide", "L", 1.8),
        item("m", "M", 1.8),
        item("s", "S", 1.8),
        item("m2", "M", 1.8),
        item("next", "L", 1.8),
      ],
      1248,
    )[0];
    expect(row.kind).toBe("tall");
    expect(row.height).toBe(288);
    expect(row.cells[0].width / row.cells[2].width).toBeCloseTo(1.75 / 0.8);
    expect(row.cells[1].width / row.cells[2].width).toBeCloseTo(1.05 / 0.8);
  });

  it("re-packs a hasMore pagination-tail straggler when the next page arrives", () => {
    const firstPage = [item("tail", "M", 1)];
    const before = justify(firstPage, 1248);
    expect(before).toHaveLength(1);
    expect(before[0].kind).toBe("standard");
    expect(fillRatio(before[0], 1248)).toBeLessThan(0.2);

    // Grid's layoutKey changes after the appended items land, so this is the
    // second justify() input seen by the memoized layout.
    const after = justify(
      [
        ...firstPage,
        item("portrait", "L", 1),
        item("next-m", "M", 1),
        item("next-s", "S", 1),
        item("terminal", "L", 1.8),
      ],
      1248,
    );
    expect(after[0].kind).toBe("tall");
    expect(after[0].cells.map((cell) => cell.item.item_id)).toEqual([
      "tail",
      "portrait",
      "next-m",
      "next-s",
    ]);
    expect(fillRatio(after[0], 1248)).toBeCloseTo(1, 5);
  });
});

describe("v2 L-run bands", () => {
  it("interpolates the ratified aspect weights and uses 1.0 for text-only Ls", () => {
    expect(largeRunWeight(item("3:4", "L", 0.75))).toBeCloseTo(0.9);
    expect(largeRunWeight(item("1:1", "L", 1))).toBeCloseTo(1);
    expect(largeRunWeight(item("5:4", "L", 1.25))).toBeCloseTo(1.1);
    expect(largeRunWeight(item("3:2", "L", 1.5))).toBeCloseTo(1.2);
    expect(largeRunWeight(item("16:9", "L", 16 / 9))).toBeCloseTo(1.5);
    expect(largeRunWeight(item("2:1", "L", 2))).toBeCloseTo(1.6);
    expect(largeRunWeight(item("narrow", "L", 0.5))).toBeCloseTo(0.9);
    expect(largeRunWeight(item("wide", "L", 3))).toBeCloseTo(1.6);
    expect(largeRunWeight(item("text", "L", 1, false))).toBeCloseTo(1);
  });

  it("packs a fully stratified feed into 3-up hero rows with no lone L bands", () => {
    const large = [
      item("l0", "L", 0.75),
      item("l1", "L", 1.5),
      item("l2-video", "L", 16 / 9),
      item("l3-text", "L", 1, false),
      item("l4", "L", 1),
      item("l5", "L", 2),
      item("l6", "L", 1),
    ];
    const medium = Array.from({ length: 6 }, (_, index) =>
      item(`m${index}`, "M", 1 + index / 10),
    );
    const small = Array.from({ length: 6 }, (_, index) =>
      item(`s${index}`, "S", 1 + index / 10),
    );
    const entries = [...large, ...medium, ...small];
    const rows = justify(entries, 1248);

    expect(rows.slice(0, 3).map((row) => [row.kind, row.cells.length])).toEqual(
      [
        ["hero", 3],
        ["hero", 3],
        ["span", 6],
      ],
    );
    expect(
      rows.some(
        (row) => row.cells.length === 1 && row.cells[0]?.item.size === "L",
      ),
    ).toBe(false);
    rows.slice(0, -1).forEach((row) => {
      expect(fillRatio(row, 1248)).toBeGreaterThanOrEqual(0.9);
    });
    expect(ids(rows)).toEqual(entries.map((entry) => entry.item_id));
  });

  it("opens a mosaic at exactly three companions but keeps exactly two in the L-run", () => {
    const withThree = justify(
      [
        item("l0", "L", 1.5),
        item("l1", "L", 1.5),
        item("transition", "L", 1),
        item("m0", "M"),
        item("s0", "S"),
        item("m1", "M"),
        item("next-l", "L", 1.8),
      ],
      1248,
    );
    expect(withThree[0].cells.map((cell) => cell.item.item_id)).toEqual([
      "l0",
      "l1",
    ]);
    expect(withThree[1].kind).toBe("tall");
    expect(withThree[1].cells.map((cell) => cell.item.item_id)).toEqual([
      "transition",
      "m0",
      "s0",
      "m1",
    ]);

    const withTwo = justify(
      [
        item("l0", "L", 1.5),
        item("l1", "L", 1.5),
        item("stays-in-run", "L", 1),
        item("m0", "M"),
        item("s0", "S"),
        item("next-l", "L", 1.8),
      ],
      1248,
    );
    expect(withTwo[0].kind).toBe("hero");
    expect(withTwo[0].cells.map((cell) => cell.item.item_id)).toEqual([
      "l0",
      "l1",
      "stays-in-run",
    ]);
  });

  it("caps portrait pairs at one before a hero row breaks the run", () => {
    const rows = justify(
      Array.from({ length: 4 }, (_, index) => item(`l${index}`, "L", 1)),
      1248,
    );
    expect(rows.map((row) => [row.kind, row.cells.length])).toEqual([
      ["pair", 2],
      ["hero", 2],
    ]);
    expect(rows[0].height).toBe(354);
    expect(rows[1].height).toBe(288);
    expect(rows.every((row) => fillRatio(row, 1248) >= 0.999)).toBe(true);
  });

  it("rebalances a four-L tail and sends only its boundary L to mosaic", () => {
    const tail = justify(
      Array.from({ length: 4 }, (_, index) => item(`tail-${index}`, "L", 1.5)),
      1248,
    );
    expect(tail.map((row) => [row.kind, row.cells.length])).toEqual([
      ["hero", 2],
      ["hero", 2],
    ]);
    expect(tail.every((row) => row.cells.length > 1)).toBe(true);

    const transition = justify(
      [
        ...Array.from({ length: 4 }, (_, index) =>
          item(`run-${index}`, "L", index === 3 ? 1 : 1.5),
        ),
        ...Array.from({ length: 5 }, (_, index) => item(`m${index}`, "M")),
      ],
      1248,
    );
    expect(
      transition.slice(0, 2).map((row) => [row.kind, row.cells.length]),
    ).toEqual([
      ["hero", 3],
      ["span", 6],
    ]);
    expect(
      transition
        .flatMap((row) => row.cells)
        .filter((cell) => cell.item.size === "L"),
    ).toHaveLength(4);
  });

  it("withholds an unresolved L pagination tail and bands it exactly once later", () => {
    const stable = [
      item("stable-0", "M"),
      item("stable-1", "S"),
      item("stable-2", "M"),
    ];
    const pending = item("pending-l", "L", 1);
    const before = justify(
      [...stable, pending, item("lookahead-1", "M")],
      1248,
      true,
    );
    expect(ids(before)).toEqual(stable.map((entry) => entry.item_id));

    const afterItems = [
      ...stable,
      pending,
      item("lookahead-1", "M"),
      item("lookahead-2", "S"),
      item("lookahead-3", "M"),
      item("next-l", "L", 1.8),
    ];
    const after = justify(afterItems, 1248, true);
    expect(after[0].cells.map((cell) => cell.item.item_id)).toEqual(
      before[0].cells.map((cell) => cell.item.item_id),
    );
    expect(
      after.flatMap((row) => row.cells).filter((cell) => cell.item === pending),
    ).toHaveLength(1);
    expect(ids(after)).toEqual(
      afterItems.slice(0, -1).map((entry) => entry.item_id),
    );

    const trailingRun = justify(
      [
        ...stable,
        item("run-0", "L", 1.5),
        item("run-1", "L", 1.5),
        item("unresolved", "L", 1.5),
        item("one-lookahead", "M"),
      ],
      1248,
      true,
    );
    expect(ids(trailingRun)).toEqual(stable.map((entry) => entry.item_id));
  });
});

describe("390px mobile bands", () => {
  it("uses no spans and matches the 246/152/112px treatments", () => {
    const rows = justify(
      [
        item("large", "L", 0.8),
        item("m0", "M"),
        item("m1", "M"),
        item("text-large", "L", 1, false),
        item("s0", "S"),
        item("s1", "S"),
        item("s2", "S"),
      ],
      390,
    );

    expect(rows.map((row) => [row.kind, row.height, row.cells.length])).toEqual(
      [
        ["tall", 246, 1],
        ["standard", 152, 2],
        ["tall", 246, 1],
        ["compact", 112, 3],
      ],
    );
    expect(rows.every((row) => row.kind !== "span")).toBe(true);
    expect(rows[0].cells[0].width).toBe(390);
    expect(rows[2].cells[0].width).toBe(390);
    expect(rows.every((row) => row.gap === 8)).toBe(true);
  });

  it("packs L-runs as a 246px leader and 208px remainder pairs", () => {
    const rows = justify(
      [
        ...Array.from({ length: 7 }, (_, index) =>
          item(`l${index}`, "L", index === 3 ? 1 : 1.5, index !== 3),
        ),
        item("m0", "M"),
        item("m1", "M"),
        item("s0", "S"),
        item("s1", "S"),
        item("s2", "S"),
      ],
      390,
    );
    expect(rows.map((row) => [row.kind, row.height, row.cells.length])).toEqual(
      [
        ["tall", 246, 1],
        ["hero", 208, 2],
        ["hero", 208, 2],
        ["hero", 208, 2],
        ["standard", 152, 2],
        ["compact", 112, 3],
      ],
    );
    expect(new Set(rows.map((row) => row.height))).toEqual(
      new Set([112, 152, 208, 246]),
    );
  });

  it("uses a 246px equal-width band for an adjacent portrait pair", () => {
    const rows = justify(
      [item("portrait-0", "L", 1), item("portrait-1", "L", 1.2)],
      390,
    );
    expect(rows.map((row) => [row.kind, row.height, row.cells.length])).toEqual(
      [["pair", 246, 2]],
    );
    expect(rows[0].cells[0].width).toBeCloseTo(191, 5);
    expect(rows[0].cells[1].width).toBeCloseTo(191, 5);
  });
});
