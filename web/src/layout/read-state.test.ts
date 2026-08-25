import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify } from "./justified";
import {
  fullyPassedRows,
  intersectingRowIDs,
  readStateEnabled,
  shouldLoadNextPage,
  shouldMarkAtBottom,
  shouldShowEndCard,
} from "./read-state";

const make = (id: number): Item => ({
  item_id: `${id}`,
  feed_id: "f",
  url: "x",
  title: "x",
  summary_source: "",
  published_ts: `2026-08-${20 - id}T00:00:00Z`,
  fetched_ts: "",
  has_body: false,
  extract_quality: 0,
  score: 0.5,
  size: "S",
  read: false,
  signal: 0,
  hearted: false,
});

describe("read state", () => {
  it("is completely disabled in the archive", () => {
    expect(readStateEnabled(false)).toBe(true);
    expect(readStateEnabled(true)).toBe(false);
  });

  it("only returns newly fully-passed rows", () => {
    const rows = justify(
      Array.from({ length: 18 }, (_, i) => make(i)),
      500,
    );
    const first = fullyPassedRows(rows, -1, rows[1].top + rows[1].height + 1);
    expect(first.rows.length).toBe(2);
    const second = fullyPassedRows(
      rows,
      first.lastIndex,
      rows[2].top + rows[2].height + 1,
    );
    expect(second.rows.length).toBe(1);
  });

  it("treats a two-row hero mosaic as one read and intersection band", () => {
    const hero = {
      ...make(100),
      size: "L" as const,
      media_url: "https://example.com/hero.jpg",
      media_w: 100,
      media_h: 100,
    };
    const companions = Array.from({ length: 5 }, (_, index) => make(index));
    const rows = justify([hero, ...companions], 1248);
    expect(rows[0].kind).toBe("span");

    expect(fullyPassedRows(rows, -1, rows[0].height).rows).toHaveLength(0);
    expect(
      fullyPassedRows(rows, -1, rows[0].height + 1).rows[0].cells.map(
        (cell) => cell.item.item_id,
      ),
    ).toEqual([hero, ...companions].map((entry) => entry.item_id));
    expect(intersectingRowIDs(rows, rows[0].height / 2, 1)).toEqual(
      [hero, ...companions].map((entry) => entry.item_id),
    );
  });

  it("marks every L in a hero row as one band", () => {
    const large = Array.from({ length: 3 }, (_, index) => ({
      ...make(index + 200),
      size: "L" as const,
      media_url: `https://example.com/${index}.jpg`,
      media_w: 150,
      media_h: 100,
    }));
    const rows = justify(large, 1248);
    expect(rows.map((row) => [row.kind, row.height])).toEqual([["hero", 288]]);
    expect(fullyPassedRows(rows, -1, 288).rows).toHaveLength(0);
    expect(
      fullyPassedRows(rows, -1, 289).rows[0].cells.map(
        (cell) => cell.item.item_id,
      ),
    ).toEqual(large.map((entry) => entry.item_id));
  });

  it("does not auto-mark a short non-scrollable list", () => {
    expect(shouldMarkAtBottom(true, 0, 500, 500)).toBe(false);
  });

  it("does not re-trigger after a programmatic pill insertion scroll", () => {
    expect(shouldMarkAtBottom(false, 500, 500, 1_000)).toBe(false);
  });

  it("marks a user-scrolled long list at the bottom", () => {
    expect(shouldMarkAtBottom(true, 499, 500, 1_000)).toBe(true);
  });

  it("keeps paging before the end card appears", () => {
    expect(shouldLoadNextPage(true, 500, 500, 1_000)).toBe(true);
    expect(shouldShowEndCard(true)).toBe(false);
    expect(shouldLoadNextPage(false, 500, 500, 1_000)).toBe(false);
    expect(shouldShowEndCard(false)).toBe(true);
  });

  it("finds the rows intersecting the viewport remainder", () => {
    const rows = justify(
      Array.from({ length: 18 }, (_, i) => make(i)),
      500,
    );
    const viewportTop = rows[1].top + 20;
    const ids = intersectingRowIDs(rows, viewportTop, rows[1].height + 30, 14);
    expect(ids).toEqual([
      ...rows[1].cells.map((cell) => cell.item.item_id),
      ...rows[2].cells.map((cell) => cell.item.item_id),
    ]);
  });
});
