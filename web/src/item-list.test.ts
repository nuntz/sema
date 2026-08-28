import { describe, expect, it } from "vitest";
import {
  finishAndClearGrid,
  includeReadForGrid,
  mergeNewItems,
  pollCandidates,
  prependGridIDs,
  unreadIDsAfter,
  updateRead,
  visibleItemIDs,
} from "./item-list";
import { readVisualState } from "./layout/read-state";
import type { Item } from "./types";

const make = (itemID: string): Item => ({
  item_id: itemID,
  feed_id: "feed",
  url: `https://example.com/${itemID}`,
  title: itemID,
  summary_source: "",
  published_ts: "2026-08-22T00:00:00Z",
  fetched_ts: "2026-08-22T00:00:00Z",
  has_body: false,
  extract_quality: 0,
  score: 0.5,
  size: "M",
  read: false,
  signal: 0,
  hearted: false,
});

describe("item list", () => {
  it("lets the server fill unread pages before returning them", () => {
    expect(includeReadForGrid(true)).toBe(false);
    expect(includeReadForGrid(false)).toBe(true);
  });

  it("inserts pill items above row zero without replacing existing state", () => {
    const existing = { ...make("existing"), read: true, signal: 1 as const };
    const current = [existing, make("older")];
    const incoming = [make("newest"), make("newer"), make("existing")];

    const merged = mergeNewItems(current, incoming);

    expect(merged.map((item) => item.item_id)).toEqual([
      "newest",
      "newer",
      "existing",
      "older",
    ]);
    expect(merged[2]).toBe(existing);
  });

  it("does not update the list when the first page has nothing new", () => {
    const current = [make("existing")];
    expect(mergeNewItems(current, [make("existing")])).toBe(current);
  });

  it("deduplicates repeated incoming items", () => {
    expect(mergeNewItems([], [make("new"), make("new")])).toHaveLength(1);
  });

  it("offers only newly fetched items in the pill", () => {
    const loaded = [{ ...make("loaded"), fetched_ts: "2026-08-22T10:00:00Z" }];
    const incoming = [
      { ...make("new"), fetched_ts: "2026-08-22T10:01:00Z" },
      { ...make("older-page-fill"), fetched_ts: "2026-08-22T09:59:00Z" },
    ];
    expect(pollCandidates(loaded, [], incoming, true)).toEqual([incoming[0]]);
  });

  it("offers read candidates only in the all-items pill", () => {
    const loaded = [{ ...make("loaded"), fetched_ts: "2026-08-22T10:00:00Z" }];
    const incoming = [
      { ...make("new-unread"), fetched_ts: "2026-08-22T10:02:00Z" },
      {
        ...make("new-read"),
        fetched_ts: "2026-08-22T10:01:00Z",
        read: true,
      },
    ];

    expect(pollCandidates(loaded, [], incoming, true)).toEqual([incoming[0]]);
    expect(pollCandidates(loaded, [], incoming, false)).toEqual(incoming);
  });

  it("keeps session-read items in the unread-only grid snapshot", () => {
    const items = [make("unread"), { ...make("already-read"), read: true }];
    const visible = visibleItemIDs(items, true);
    const read = updateRead(items, visible, true);

    expect(visible).toEqual(["unread"]);
    expect(
      visible.map((id) => read.find((item) => item.item_id === id)?.read),
    ).toEqual([true]);
  });

  it("marks every remaining loaded unread item from the caught-up card", () => {
    const items = [
      make("one"),
      { ...make("already-read"), read: true },
      make("three"),
    ];
    const ids = visibleItemIDs(items, true);
    const read = updateRead(items, ids, true);

    expect(ids).toEqual(["one", "three"]);
    expect(read.every((item) => item.read)).toBe(true);
  });

  it("clears the grid while preserving an exact undo snapshot", () => {
    const result = finishAndClearGrid(["first", "dimmed", "last"], "last", 412);

    expect(result.ids).toEqual([]);
    expect(result.snapshot).toEqual({
      ids: ["first", "dimmed", "last"],
      focusedID: "last",
      scrollTop: 412,
    });
  });

  it("restores the cleared snapshot and unread flags on undo", () => {
    const originalIDs = ["one", "two", "three"];
    const items = [make("one"), make("two"), make("three")];
    const read = updateRead(items, ["one", "three"], true);
    const { snapshot } = finishAndClearGrid(originalIDs, "three", 300);
    const restored = updateRead(read, ["one", "three"], false);

    expect(snapshot.ids).toEqual(originalIDs);
    expect(restored.map((item) => item.read)).toEqual([false, false, false]);
  });

  it("prepends pill items onto a cleared grid", () => {
    expect(prependGridIDs([], ["newest", "newer"])).toEqual([
      "newest",
      "newer",
    ]);
  });

  it("undoes an explicit read batch without changing item order", () => {
    const items = [make("one"), make("two"), make("three")];
    const read = updateRead(items, ["one", "two"], true);
    const undone = updateRead(read, ["one", "two"], false);

    expect(read.map((item) => item.read)).toEqual([true, true, false]);
    expect(undone).toEqual(items);
  });

  it("flips the all-items dot when an item is explicitly toggled", () => {
    const items = [make("focused")];
    const marked = updateRead(items, ["focused"], true);
    const restored = updateRead(marked, ["focused"], false);

    expect(readVisualState("all-items", items[0].read).unreadDot).toBe(true);
    expect(readVisualState("all-items", marked[0].read).unreadDot).toBe(false);
    expect(readVisualState("all-items", restored[0].read).unreadDot).toBe(true);
  });

  it("selects every unread item below the focused item", () => {
    const items = [
      make("above"),
      make("focused"),
      { ...make("read-below"), read: true },
      make("unread-below"),
    ];
    expect(unreadIDsAfter(items, "focused")).toEqual(["unread-below"]);
  });
});
