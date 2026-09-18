import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify, type LayoutRow } from "./justified";
import {
  cellLandingTop,
  cellRects,
  type LayoutRect,
  nearestCell,
  nearestPageCell,
  nextGridPageTop,
  previousGridPageTop,
} from "./navigation";

describe("grid page scroll target", () => {
  const rows: LayoutRow[] = [0, 210, 420, 630].map((top) => ({
    top,
    height: 200,
    gap: 10,
    kind: "standard",
    cells: [],
  }));

  it("lands the first row that is not fully visible with the previous row ending at the top edge", () => {
    // Viewport 0..500: row 420 sits at 434..634 and is cut off; land at 424 so
    // row 210 ends exactly at the top edge and the 10px gap is the air above.
    expect(nextGridPageTop(rows, 0, 500)).toBe(424);
    expect(nextGridPageTop(rows, 214, 500)).toBe(634);
  });

  it("steps to the next row when the bottom edge is in a gap or at a row edge", () => {
    for (const height of [214, 220, 224]) {
      expect(nextGridPageTop(rows, 0, height)).toBe(214);
    }
    expect(nextGridPageTop([], 0, 500)).toBe(500);
  });

  it("continues through rows taller than the viewport", () => {
    const tallRows = [{ ...rows[0], height: 1000 }];
    expect(nextGridPageTop(tallRows, 0, 500)).toBe(500);
    expect(nextGridPageTop(tallRows, 500, 500)).toBe(1000);
    expect(nextGridPageTop(tallRows, 300, 500)).toBe(800);
  });

  it("pages up onto the first row that fits, with the previous row ending at the top edge", () => {
    expect(previousGridPageTop(rows, 634, 500)).toBe(214);
    expect(previousGridPageTop(rows, 424, 500)).toBe(0);
    expect(previousGridPageTop(rows, 700, 500)).toBe(214);
  });

  it("returns to the page a page-down came from", () => {
    for (const start of [0, 214]) {
      const landed = nextGridPageTop(rows, start, 500);
      expect(previousGridPageTop(rows, landed, 500)).toBe(start);
    }
  });

  it("pages up to the very top past a slot above the first row", () => {
    const slotRows = rows.map((row) => ({ ...row, top: row.top + 54 }));
    expect(previousGridPageTop(slotRows, 268, 500)).toBe(0);
    expect(previousGridPageTop(slotRows, 688, 500)).toBe(268);
  });

  it("pages up through rows taller than the viewport and stops at the top", () => {
    const tallRows = [{ ...rows[0], height: 1000 }];
    expect(previousGridPageTop(tallRows, 1000, 500)).toBe(500);
    expect(previousGridPageTop(tallRows, 500, 500)).toBe(0);
    expect(previousGridPageTop(tallRows, 300, 500)).toBe(0);
    expect(previousGridPageTop([], 300, 500)).toBe(0);
  });
});

describe("arrow navigation landing", () => {
  const row = { top: 420, height: 200, gap: 10 };
  const lead: LayoutRect = {
    id: "story:s",
    row,
    left: 0,
    right: 100,
    top: 420,
    bottom: 520, // headlines occupy 520..620
    centerX: 50,
    centerY: 470,
  };

  it("scrolls the whole row into view from below, headlines included", () => {
    // Viewport 0..500: the lead's bottom is cut off. Land so the next row's
    // top edge (layout 630, drawn at 644) meets the viewport bottom.
    expect(cellLandingTop(lead, 0, 500)).toBe(144);
  });

  it("scrolls the row into view from above with the previous row at the top edge", () => {
    expect(cellLandingTop(lead, 900, 500)).toBe(424);
  });

  it("keeps a target visible inside a row taller than the viewport", () => {
    const tall = { ...lead, row: { top: 420, height: 1000, gap: 10 } };
    // The lead's own top lands; its bottom (544) is still inside 424..724.
    expect(cellLandingTop(tall, 0, 300)).toBe(424);
    const headline = { ...tall, top: 1300, bottom: 1352 };
    expect(cellLandingTop(headline, 2000, 300)).toBe(1352 + 24 - 300);
  });

  it("leaves a visible target alone and tolerates DOM rects without a row", () => {
    expect(cellLandingTop(lead, 400, 500)).toBe(400);
    const { row: _row, ...bare } = lead;
    expect(cellLandingTop(bare, 0, 300)).toBe(534 - 300);
  });
});

const item = (index: number, size: Item["size"], ratio = 1.5): Item => ({
  item_id: String(index),
  feed_id: "f",
  url: "https://example.com",
  title: `Item ${index}`,
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
  media_url: `https://example.com/${index}.jpg`,
  media_w: ratio * 100,
  media_h: 100,
});

describe("geometric grid navigation", () => {
  it("navigates only displayed story headlines and crosses into the next row", () => {
    const lead = item(0, "L");
    const rows: LayoutRow[] = [
      {
        top: 0,
        height: 300,
        gap: 10,
        kind: "standard",
        cells: [
          {
            item: lead,
            story: {
              story_id: "story",
              source_count: 4,
              order_key: 1,
              size: "L",
              items: [lead, item(1, "S"), item(2, "S"), item(3, "S")],
            },
            left: 0,
            width: 200,
            effectiveSize: "L",
            headlineHeight: 104,
            headlineItemCount: 2,
          },
        ],
      },
      {
        top: 310,
        height: 100,
        gap: 10,
        kind: "standard",
        cells: [
          { item: item(4, "S"), left: 0, width: 200, effectiveSize: "S" },
        ],
      },
    ];
    expect(cellRects(rows).map(({ id }) => id)).toEqual([
      "story:story",
      "1",
      "2",
      "4",
    ]);
    expect(nearestCell(rows, "story:story", "down")).toBe("1");
    expect(nearestCell(rows, "1", "down")).toBe("2");
    expect(nearestCell(rows, "2", "down")).toBe("4");
    expect(nearestCell(rows, "4", "up")).toBe("2");
    expect(nearestCell(rows, "1", "up")).toBe("story:story");
  });

  it("does not move vertically into a same-row neighbor with a different height", () => {
    const rows: LayoutRow[] = [
      {
        top: 0,
        height: 300,
        gap: 10,
        kind: "standard",
        cells: [
          {
            item: item(0, "S"),
            left: 0,
            width: 100,
            height: 100,
            effectiveSize: "S",
          },
          {
            item: item(1, "L"),
            left: 110,
            width: 100,
            height: 300,
            effectiveSize: "L",
          },
        ],
      },
    ];
    expect(nearestCell(rows, "0", "down")).toBe("0");
    expect(nearestCell(rows, "1", "up")).toBe("1");
    expect(nearestCell(rows, "0", "right")).toBe("1");
  });
  const rect = (
    id: string,
    left: number,
    top: number,
    width = 100,
    height = 100,
  ): LayoutRect => ({
    id,
    left,
    top,
    right: left + width,
    bottom: top + height,
    centerX: left + width / 2,
    centerY: top + height / 2,
  });

  it("pages to the nearest visible position across unequal cells and columns", () => {
    const rects = [
      rect("old", 200, -200),
      rect("left", 0, 100),
      rect("right", 200, 100),
      rect("lower", 200, 300),
    ];
    expect(nearestPageCell(rects, 250, 170, 0, 500)).toBe("right");
    expect(nearestPageCell(rects, 50, 170, 0, 500)).toBe("left");
    expect(nearestPageCell(rects, 250, 380, 0, 500)).toBe("lower");
  });

  it("prefers a fully visible cell over a clipped edge cell", () => {
    expect(
      nearestPageCell(
        [rect("clipped", 0, -50), rect("visible", 0, 60)],
        50,
        20,
        0,
        500,
      ),
    ).toBe("visible");
  });

  it("supports cells taller than the viewport and an end card with no cells", () => {
    expect(
      nearestPageCell([rect("tall", 0, -100, 100, 800)], 50, 250, 0, 500),
    ).toBe("tall");
    expect(
      nearestPageCell([rect("above", 0, -100)], 50, 250, 0, 500),
    ).toBeUndefined();
  });
  it("moves down from a spanning hero into its lower adjacent sub-row", () => {
    const entries = [
      item(0, "L", 1),
      ...Array.from({ length: 6 }, (_, index) => item(index + 1, "S")),
      item(7, "M"),
    ];
    const rows = justify(entries, 1248);

    expect(nearestCell(rows, "0", "down")).toBe("4");
    expect(nearestCell(rows, "1", "down")).toBe("4");
    expect(nearestCell(rows, "4", "up")).toBe("1");
    expect(
      rows.flatMap((row) => row.cells.map((cell) => cell.item.item_id)),
    ).toEqual(entries.map((entry) => entry.item_id));
  });

  it("keeps left/right on the visual line and crosses bands vertically", () => {
    const entries = [
      item(0, "L", 1),
      ...Array.from({ length: 6 }, (_, index) => item(index + 1, "S")),
      item(7, "M"),
      item(8, "S"),
    ];
    const rows = justify(entries, 1248);

    expect(nearestCell(rows, "1", "right")).toBe("2");
    expect(nearestCell(rows, "3", "right")).toBe("3");
    expect(nearestCell(rows, "5", "left")).toBe("4");
    expect(nearestCell(rows, "4", "down")).toBe("8");
  });

  it("moves geometrically across full-width mobile L and pair bands", () => {
    const rows = justify(
      [item(0, "L", 1), item(1, "M"), item(2, "S"), item(3, "M")],
      390,
    );
    expect(rows.every((row) => row.kind !== "span")).toBe(true);
    expect(nearestCell(rows, "0", "down")).toBe("1");
    expect(nearestCell(rows, "1", "down")).toBe("3");
    expect(nearestCell(rows, "2", "down")).toBe("3");
  });

  it("traverses hero rows left-to-right and crosses into a mosaic band", () => {
    const rows = justify(
      [
        item(0, "L", 1.5),
        item(1, "L", 1.5),
        item(2, "L", 1.5),
        item(3, "L", 1),
        ...Array.from({ length: 5 }, (_, index) => item(index + 4, "M")),
      ],
      1248,
    );
    expect(rows.map((row) => row.kind)).toEqual(["hero", "span"]);
    expect(nearestCell(rows, "0", "right")).toBe("1");
    expect(nearestCell(rows, "1", "right")).toBe("2");
    expect(nearestCell(rows, "2", "down")).toBe("6");
    expect(rows[1].cells.map((cell) => cell.item.item_id)).toContain(
      nearestCell(rows, "1", "down"),
    );
  });
});
