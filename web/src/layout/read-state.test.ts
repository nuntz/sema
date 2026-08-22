import { describe, expect, it } from "vitest";
import type { Item } from "../types";
import { justify } from "./justified";
import { fullyPassedRows } from "./read-state";

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
});
