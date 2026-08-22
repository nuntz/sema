import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify } from "./justified";
import { chronoBoundary, chronoRead, fullyPassedRows } from "./read-state";

const make = (id: number): Item => ({
  item_id: `${id}`,
  feed_id: "f",
  url: "x",
  title: "x",
  published_ts: `2026-08-${20 - id}T00:00:00Z`,
  fetched_ts: "",
  has_body: false,
  score: 0.5,
  size: "S",
  read: false,
  signal: 0,
});

describe("read state", () => {
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

  it("uses the oldest timestamp in the passed row", () => {
    const row = justify([make(1), make(3), make(2)], 800)[0];
    expect(chronoBoundary(row)).toBe("2026-08-17T00:00:00Z");
  });

  it("marks items at and above the descending-time boundary read", () => {
    const boundary = "2026-08-18T00:00:00Z";
    expect(chronoRead("2026-08-19T00:00:00Z", boundary)).toBe(true);
    expect(chronoRead(boundary, boundary)).toBe(true);
    expect(chronoRead("2026-08-17T00:00:00Z", boundary)).toBe(false);
  });
});
