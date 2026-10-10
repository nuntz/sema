import { describe, expect, it } from "vitest";
import { pageHasRead } from "./item-session";

describe("pageHasRead", () => {
  it("trusts has_read from the unread index", () => {
    expect(pageHasRead({ items: [], next_cursor: null, has_read: true })).toBe(
      true,
    );
  });

  it("still accepts a read anchor from older responses", () => {
    expect(
      pageHasRead({
        items: [],
        next_cursor: null,
        read_anchor: { item_id: "a", published_ts: "2026-10-09T00:00:00Z" },
      }),
    ).toBe(true);
  });

  it("is false when nothing has been read", () => {
    expect(pageHasRead({ items: [], next_cursor: null, has_read: false })).toBe(
      false,
    );
    expect(pageHasRead({ items: [], next_cursor: null })).toBe(false);
  });
});
