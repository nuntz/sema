import { describe, expect, it } from "vitest";
import { updateRead } from "../item-list";
import type { Item } from "../types";
import { justify } from "./justified";
import {
  automaticReadEnabled,
  caughtUpBoundary,
  caughtUpLabel,
  endMarkActionEnabled,
  fullyPassedRows,
  gridReadStateContext,
  intersectingRowIDs,
  readVisualState,
  scrollReadCandidates,
  shouldLoadNextPage,
  shouldLoadToFillViewport,
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
  it("encodes dimming and dots by view context", () => {
    expect(readVisualState("unread", false)).toEqual({
      dimmed: false,
      unreadDot: false,
    });
    expect(readVisualState("unread", true)).toEqual({
      dimmed: true,
      unreadDot: false,
    });
    expect(readVisualState("all-items", false)).toEqual({
      dimmed: false,
      unreadDot: true,
    });
    expect(readVisualState("all-items", true)).toEqual({
      dimmed: false,
      unreadDot: false,
    });
    for (const context of ["search", "archive"] as const) {
      expect(readVisualState(context, false)).toEqual({
        dimmed: false,
        unreadDot: false,
      });
      expect(readVisualState(context, true)).toEqual({
        dimmed: false,
        unreadDot: false,
      });
    }
  });

  it("enables automatic read marking only in the unread grid", () => {
    expect(gridReadStateContext(false, true)).toBe("unread");
    expect(gridReadStateContext(false, false)).toBe("all-items");
    expect(gridReadStateContext(true, true)).toBe("archive");
    expect(automaticReadEnabled("unread")).toBe(true);
    expect(automaticReadEnabled("all-items")).toBe(false);
    expect(automaticReadEnabled("search")).toBe(false);
    expect(automaticReadEnabled("archive")).toBe(false);
    expect(endMarkActionEnabled("unread")).toBe(true);
    expect(endMarkActionEnabled("all-items")).toBe(false);
    expect(endMarkActionEnabled("archive")).toBe(false);
  });

  it("positions the caught-up divider above the newest read item in All", () => {
    const loaded = [make(0), make(1), { ...make(2), read: true }, make(3)];

    expect(caughtUpBoundary("all-items", "chrono", loaded, loaded)).toEqual({
      count: 2,
      beforeItemID: "2",
    });
  });

  it("positions the unread divider between items straddling the read item", () => {
    const loaded = [make(0), make(1), { ...make(2), read: true }, make(3)];
    const unread = loaded.filter((item) => !item.read);

    expect(caughtUpBoundary("unread", "chrono", loaded, unread)).toEqual({
      count: 2,
      beforeItemID: "3",
    });
  });

  it("positions the unread divider from a server-loaded read anchor", () => {
    const unread = [make(0), make(1), make(3), make(4)];

    expect(
      caughtUpBoundary("unread", "chrono", unread, unread, {
        item_id: "anchor",
        published_ts: make(2).published_ts,
      }),
    ).toEqual({ count: 2, beforeItemID: "3" });
  });

  it("hides the caught-up divider outside Latest grid views", () => {
    const loaded = [make(0), { ...make(1), read: true }, make(2)];

    expect(
      caughtUpBoundary("unread", "interest", loaded, loaded),
    ).toBeUndefined();
    expect(
      caughtUpBoundary("all-items", "interest", loaded, loaded),
    ).toBeUndefined();
    expect(
      caughtUpBoundary("search", "chrono", loaded, loaded),
    ).toBeUndefined();
    expect(
      caughtUpBoundary("archive", "chrono", loaded, loaded),
    ).toBeUndefined();
  });

  it("does not guess when no read anchor is loaded or nothing is above it", () => {
    const unread = [make(0), make(1), make(2)];
    expect(
      caughtUpBoundary("all-items", "chrono", unread, unread),
    ).toBeUndefined();
    const anchorLoadedLater = [...unread, { ...make(3), read: true }, make(4)];
    expect(
      caughtUpBoundary(
        "all-items",
        "chrono",
        anchorLoadedLater,
        anchorLoadedLater,
      ),
    ).toEqual({ count: 3, beforeItemID: "3" });

    const newestRead = [{ ...make(0), read: true }, make(1)];
    expect(
      caughtUpBoundary("all-items", "chrono", newestRead, newestRead),
    ).toBeUndefined();
    expect(
      caughtUpBoundary("unread", "chrono", unread, unread),
    ).toBeUndefined();
  });

  it("labels the caught-up divider", () => {
    const boundary = { count: 34, beforeItemID: "anchor" };
    expect(caughtUpLabel(boundary.count)).toBe(
      "New since you last caught up · 34",
    );
  });

  it("recomputes the boundary after m-style toggles and undo", () => {
    const initial = [make(0), make(1), { ...make(2), read: true }, make(3)];
    const marked = updateRead(initial, ["1"], true);
    const undone = updateRead(marked, ["1"], false);
    const noReads = updateRead(undone, ["2"], false);

    expect(
      caughtUpBoundary("all-items", "chrono", initial, initial)?.count,
    ).toBe(2);
    expect(caughtUpBoundary("all-items", "chrono", marked, marked)).toEqual({
      count: 1,
      beforeItemID: "1",
    });
    expect(caughtUpBoundary("all-items", "chrono", undone, undone)?.count).toBe(
      2,
    );
    expect(
      caughtUpBoundary("all-items", "chrono", noReads, noReads),
    ).toBeUndefined();
  });

  it("recomputes an unread boundary against the persisted anchor", () => {
    const initial = [make(0), make(1), make(3)];
    const anchor = {
      item_id: "anchor",
      published_ts: make(2).published_ts,
    };
    const marked = updateRead(initial, ["1"], true);
    const undone = updateRead(marked, ["1"], false);

    expect(
      caughtUpBoundary("unread", "chrono", initial, initial, anchor),
    ).toEqual({ count: 2, beforeItemID: "3" });
    expect(
      caughtUpBoundary("unread", "chrono", marked, marked, anchor),
    ).toEqual({ count: 1, beforeItemID: "1" });
    expect(
      caughtUpBoundary("unread", "chrono", undone, undone, anchor),
    ).toEqual({ count: 2, beforeItemID: "3" });
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

  it("returns zero read candidates when all-items is scrolled", () => {
    const rows = justify(
      Array.from({ length: 18 }, (_, i) => make(i)),
      500,
    );
    expect(
      scrollReadCandidates(
        "all-items",
        rows,
        rows[2].top + rows[2].height + 1,
        500,
        2_000,
        true,
        new Set(),
        new Set(),
      ),
    ).toEqual([]);
  });

  it("keeps scroll-past candidates unchanged in the unread view", () => {
    const rows = justify(
      Array.from({ length: 18 }, (_, i) => make(i)),
      500,
    );
    const scrollTop = rows[2].top + rows[2].height + 1;
    expect(
      scrollReadCandidates(
        "unread",
        rows,
        scrollTop,
        500,
        2_000,
        true,
        new Set([rows[0].cells[0].item.item_id]),
        new Set([rows[1].cells[0].item.item_id]),
      ),
    ).toEqual([
      ...rows[0].cells.slice(1).map((cell) => cell.item.item_id),
      ...rows[1].cells.slice(1).map((cell) => cell.item.item_id),
      ...rows[2].cells.map((cell) => cell.item.item_id),
    ]);
  });

  it("keeps paging before the end card appears", () => {
    expect(shouldLoadNextPage(true, 500, 500, 1_000)).toBe(true);
    expect(shouldShowEndCard(true)).toBe(false);
    expect(shouldLoadNextPage(false, 500, 500, 1_000)).toBe(false);
    expect(shouldShowEndCard(false)).toBe(true);
  });

  it("fills a sparse unread viewport without prefetching scrollable content", () => {
    expect(shouldLoadToFillViewport(true, 640, 800)).toBe(true);
    expect(shouldLoadToFillViewport(true, 800, 800)).toBe(true);
    expect(shouldLoadToFillViewport(true, 801, 800)).toBe(false);
    expect(shouldLoadToFillViewport(false, 640, 800)).toBe(false);
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
