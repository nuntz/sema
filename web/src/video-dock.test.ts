import { describe, expect, it, vi } from "vitest";
import type { Item } from "./types";
import {
  createVideoDock,
  DWELL_THRESHOLD_MS,
  expandTarget,
} from "./video-dock";

const item = (id: string, archived = false) =>
  ({ item_id: id, title: id, archived }) as Item;

function setup() {
  let clock = 0;
  const reports: Array<[string, number]> = [];
  const dock = createVideoDock(() => clock);
  dock.setReporter((itemID, ms) => reports.push([itemID, ms]));
  let seconds = 0;
  return {
    attach: () => dock.attach(() => seconds),
    dock,
    reports,
    advance: (ms: number) => {
      clock += ms;
    },
    seek: (value: number) => {
      seconds = value;
    },
  };
}

describe("video dock", () => {
  it("Peeks a new Video Item as a new Play and docks it only while live", () => {
    const { dock } = setup();
    expect(dock.peek(item("a"))).toBe(true);
    expect(dock.mode()).toBe("peek");
    dock.setState("playing");
    dock.dismissPeek();
    expect(dock.mode()).toBe("dock");
    expect(dock.current()?.origin).toBe("peek");
    for (const state of ["paused", "ended", "failed", "loading"] as const) {
      dock.peek(item(state));
      dock.setState(state);
      dock.dismissPeek();
      expect(dock.current()).toBeUndefined();
    }
    dock.peek(item("b"));
    dock.setState("buffering");
    dock.dismissPeek();
    expect(dock.mode()).toBe("dock");
  });

  it("Peeking the Docked Item Expands it without a new player or Play", () => {
    const { dock } = setup();
    dock.peek(item("a"));
    dock.setState("playing");
    dock.dismissPeek();
    const key = dock.current()?.key;
    expect(dock.peek(item("a"))).toBe(false);
    expect(dock.mode()).toBe("peek");
    expect(dock.current()?.key).toBe(key);
  });

  it("a new Play replaces the Dock with a fresh player", () => {
    const { dock } = setup();
    dock.peek(item("a"));
    dock.setState("playing");
    dock.dismissPeek();
    const key = dock.current()?.key;
    expect(dock.peek(item("b"))).toBe(true);
    expect(dock.current()?.item.item_id).toBe("b");
    expect(dock.current()?.key).not.toBe(key);
    expect(dock.current()?.start).toBe(0);
  });

  it("a reader hands over position and dwell, and takes them back", () => {
    const { dock, seek, attach } = setup();
    dock.dockFromReader(item("a"), 42, 12_000, false);
    expect(dock.mode()).toBe("dock");
    expect(dock.current()).toMatchObject({ origin: "reader", start: 42 });
    expect(dock.take("other")).toBeUndefined();
    expect(dock.take("a")).toEqual({ seconds: 42, dwellMS: 12_000 });
    dock.dockFromReader(item("a"), 42, 12_000, false);
    attach();
    seek(50);
    expect(dock.take("a")).toEqual({ seconds: 50, dwellMS: 12_000 });
    expect(dock.current()).toBeUndefined();
  });

  it("accrues dwell only while Docked, live and visible, continuing a reader's total", () => {
    const { dock, advance, reports } = setup();
    dock.dockFromReader(item("a"), 0, 5_000, false);
    dock.setState("playing");
    advance(4_000);
    dock.setVisible(false);
    advance(60_000);
    dock.setVisible(true);
    advance(1_000);
    dock.setState("paused");
    expect(reports).toEqual([
      ["a", 9_000],
      ["a", 10_000],
    ]);
    advance(9_000);
    dock.setState("playing");
    advance(2_000);
    dock.close();
    expect(reports.at(-1)).toEqual(["a", 12_000]);
  });

  it("starts a Peek-born Dock at zero and pauses dwell while Expanded", () => {
    const { dock, advance, reports } = setup();
    dock.peek(item("a"));
    dock.setState("playing");
    advance(10_000);
    dock.dismissPeek();
    advance(3_000);
    dock.peek(item("a"));
    advance(10_000);
    dock.dismissPeek();
    advance(2_000);
    dock.close();
    expect(reports).toEqual([
      ["a", 3_000],
      ["a", 5_000],
    ]);
  });

  it("reports once when dwell crosses the threshold", () => {
    const { dock, advance, reports } = setup();
    dock.dockFromReader(item("a"), 0, 0, false);
    dock.setState("playing");
    advance(DWELL_THRESHOLD_MS - 1);
    dock.tick();
    expect(reports).toEqual([]);
    advance(1);
    dock.tick();
    dock.tick();
    expect(reports).toEqual([["a", DWELL_THRESHOLD_MS]]);
  });

  it("never reports dwell for an archived Item", () => {
    const { dock, advance, reports } = setup();
    dock.dockFromReader(item("a", true), 0, 0, true);
    dock.setState("playing");
    advance(5_000);
    dock.close();
    dock.peek(item("b", true));
    dock.setState("playing");
    dock.dismissPeek();
    advance(5_000);
    dock.close();
    expect(reports).toEqual([]);
  });

  it("Expands to the Peek only when it came from one and no reader is open", () => {
    const peek = { origin: "peek" as const };
    const reader = { origin: "reader" as const };
    expect(expandTarget(peek, false)).toBe("peek");
    expect(expandTarget(peek, true)).toBe("reader");
    expect(expandTarget(reader, false)).toBe("reader");
    expect(expandTarget(reader, true)).toBe("reader");
  });

  it("ignores stage callbacks after close", () => {
    const { dock, reports } = setup();
    const report = vi.fn();
    dock.setReporter(report);
    dock.close();
    dock.setState("playing");
    dock.tick();
    dock.dismissPeek();
    expect(report).not.toHaveBeenCalled();
    expect(reports).toEqual([]);
  });
});
