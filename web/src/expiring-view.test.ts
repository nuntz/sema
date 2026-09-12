import { afterEach, describe, expect, it, vi } from "vitest";
import {
  deadlineGroup,
  expiringItems,
  expiryGroups,
  nextMidnight,
} from "./expiring-view";
import { LIFETIME } from "./expiry";
import type { Item } from "./types";

afterEach(() => vi.unstubAllEnvs());
const item = (deadline: string, flags = {}) =>
  ({
    item_id: deadline,
    published_ts: new Date(Date.parse(deadline) - LIFETIME).toISOString(),
    hearted: false,
    read: false,
    ...flags,
  }) as Item;
describe("local deadline groups", () => {
  it("uses Vancouver midnight even when UTC is already tomorrow", () => {
    vi.stubEnv("TZ", "America/Vancouver");
    const now = Date.parse("2026-09-08T02:00:00Z");
    const items = [
      item("2026-09-10T01:00:00Z"),
      item("2026-09-08T07:00:00Z"),
      item("2026-09-08T06:59:59Z"),
    ];
    expect(
      expiryGroups(items, now).map((g) => [g.key, g.items.length, g.note]),
    ).toEqual([
      ["tonight", 1, "after 23:59"],
      ["tomorrow", 1, ""],
      ["later", 1, "two days or less"],
    ]);
    expect(deadlineGroup(item("2026-09-09T07:00:00Z").published_ts, now)).toBe(
      "later",
    );
  });
  it("uses calendar midnight across daylight saving changes", () => {
    vi.stubEnv("TZ", "America/Vancouver");
    expect(nextMidnight(Date.parse("2026-03-08T09:00:00Z")).toISOString()).toBe(
      "2026-03-09T07:00:00.000Z",
    );
  });
  it("orders soonest first and retains read items and excludes kept, archived and elapsed items", () => {
    const now = Date.parse("2026-09-08T12:00:00Z");
    const first = item("2026-09-08T13:00:00Z");
    const last = item("2026-09-10T12:00:00Z");
    expect(
      expiringItems(
        [
          last,
          first,
          item("2026-09-08T13:00:00Z", { hearted: true }),
          item("2026-09-08T14:00:00Z", { read: true }),
          item("2026-09-08T15:00:00Z", { archived: true }),
          item("2026-09-08T12:00:00Z"),
          item("2026-09-10T12:00:01Z"),
        ],
        now,
      ),
    ).toEqual([first, item("2026-09-08T14:00:00Z", { read: true }), last]);
  });
});
