import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify } from "./justified";

const item = (index: number, size: Item["size"], ratio = 1.5): Item => ({
  item_id: String(index),
  feed_id: "f",
  url: "https://example.com",
  title: `Item ${index}`,
  summary_source: "",
  published_ts: new Date().toISOString(),
  fetched_ts: new Date().toISOString(),
  has_body: true,
  extract_quality: 1,
  score: 0.5,
  size,
  read: false,
  signal: 0,
  hearted: false,
  media_w: ratio * 100,
  media_h: 100,
});

const usedWidth = (row: ReturnType<typeof justify>[number]) =>
  row.cells.reduce((sum, cell) => sum + cell.width, 0) +
  (row.cells.length - 1) * row.gap;

describe("justify", () => {
  it("justifies complete mixed rows but leaves the final row natural", () => {
    const width = 1180;
    const rows = justify(
      Array.from({ length: 18 }, (_, index) =>
        item(index, (["S", "M", "S", "L", "S"] as const)[index % 5]),
      ),
      width,
    );
    for (const row of rows.slice(0, -1)) {
      if (row.cells.every((cell) => cell.effectiveSize === "S")) continue;
      expect(usedWidth(row)).toBeCloseTo(width, 5);
    }
    const last = rows.at(-1);
    expect(last).toBeDefined();
    if (last) expect(usedWidth(last)).toBeLessThan(width);
  });

  it("keeps every large item large and caps rows at one", () => {
    const rows = justify(
      Array.from({ length: 12 }, (_, index) => item(index, "L", 0.8)),
      1180,
    );
    expect(rows.flatMap((row) => row.cells)).toHaveLength(12);
    for (const row of rows) {
      expect(
        row.cells.filter((cell) => cell.effectiveSize === "L"),
      ).toHaveLength(1);
    }
  });

  it("uses the handoff's L/M/S width bases", () => {
    const row = justify(
      [item(1, "L", 1), item(2, "M", 1), item(3, "S", 1)],
      1180,
    )[0];
    expect(row.cells[0].width / row.cells[2].width).toBeCloseTo(1.6 / 0.8);
    expect(row.cells[1].width / row.cells[2].width).toBeCloseTo(1.05 / 0.8);
  });

  it("caps all-small rows without stretching or overfilling", () => {
    const width = 1180;
    const rows = justify(
      Array.from({ length: 13 }, (_, index) => item(index, "S")),
      width,
    );
    for (const row of rows) {
      expect(row.cells.length).toBeLessThanOrEqual(6);
      expect(row.height).toBeLessThanOrEqual(144);
      expect(usedWidth(row)).toBeLessThan(width);
    }
  });

  it("raises desktop rows for the larger typography scale", () => {
    const rows = justify(
      Array.from({ length: 30 }, (_, index) =>
        item(index, (["S", "S", "M", "L"] as const)[index % 4]),
      ),
      1180,
    );
    const heights = rows.map((row) => row.height);
    expect(Math.min(...heights)).toBeGreaterThanOrEqual(140);
    expect(Math.max(...heights)).toBeLessThanOrEqual(204);
    expect(Math.max(...heights) - Math.min(...heights)).toBeLessThanOrEqual(64);
  });

  it("caps small runs at four inside mixed rows", () => {
    const rows = justify(
      [
        item(0, "M"),
        ...Array.from({ length: 5 }, (_, index) => item(index + 1, "S")),
        item(6, "M"),
      ],
      1180,
    );
    for (const row of rows) {
      if (row.cells.every((cell) => cell.effectiveSize === "S")) continue;
      let run = 0;
      for (const cell of row.cells) {
        run = cell.effectiveSize === "S" ? run + 1 : 0;
        expect(run).toBeLessThanOrEqual(4);
      }
    }
  });

  it("uses the mobile 8px gap and three-up all-small exception", () => {
    const rows = justify(
      Array.from({ length: 6 }, (_, index) => item(index, "S")),
      272,
    );
    expect(rows).toHaveLength(2);
    expect(rows.every((row) => row.gap === 8 && row.cells.length === 3)).toBe(
      true,
    );
    expect(rows.every((row) => row.height <= 144)).toBe(true);
  });

  it("gives a mobile large item its own row when its neighbour would be under 96px", () => {
    const rows = justify([item(1, "L"), item(2, "S"), item(3, "M")], 272);
    expect(rows[0].cells.map((cell) => cell.effectiveSize)).toEqual(["L"]);
    expect(rows[0].cells[0].width).toBe(272);
  });
});
