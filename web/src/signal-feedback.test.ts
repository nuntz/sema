import { describe, expect, it } from "vitest";
import {
  buryDisabled,
  clusterActions,
  nextSignalNotice,
  signalActionLabel,
  signalLabel,
  signalWhy,
} from "./signal-feedback";

describe("cell feedback", () => {
  it.each([
    [1, false, "More like this from tomorrow"],
    [-1, false, "Fewer like this · undo"],
    [1, true, "More of this story from tomorrow"],
    [-1, true, "Fewer of this story · undo"],
    [0, false, ""],
    [0, true, ""],
  ] as const)("uses reader copy for %s, story=%s", (value, story, text) => {
    expect(signalWhy(value, story)).toBe(text);
  });
  it("keeps corner labels textual and neutral empty", () => {
    expect([-1, 0, 1].map((value) => signalLabel(value as -1 | 0 | 1))).toEqual(
      ["buried", "", "boosted"],
    );
  });
  it("fits the action cluster to the cell size", () => {
    expect(clusterActions("S")).toEqual(["boost", "bury"]);
    for (const size of ["M", "L"] as const)
      expect(clusterActions(size)).toEqual(["boost", "bury", "keep", "more"]);
  });
  it("blocks bury for kept items and explains the disabled action", () => {
    expect(buryDisabled({ hearted: true })).toBe(true);
    expect(buryDisabled({ hearted: false })).toBe(false);
    expect(signalActionLabel(1, "bury", true)).toBe(
      "Kept items can't be buried",
    );
    expect(signalActionLabel(0, "boost", false)).toBe("Boost (+)");
    expect(signalActionLabel(1, "boost", true)).toBe("Boosted (+ to undo)");
    expect(signalActionLabel(0, "bury", false)).toBe("Bury (−)");
    expect(signalActionLabel(-1, "bury", false)).toBe("Buried (− to undo)");
  });
});

describe("first-five feedback", () => {
  it("shows the first five actions and retires at the lifetime limit", () => {
    for (let count = 0; count < 5; count++)
      expect(nextSignalNotice(count, count, "item", 1)?.text).toBe(
        "Boosted. Sizes settle overnight.",
      );
    expect(nextSignalNotice(5, 6, "item", -1)).toBeUndefined();
    expect(nextSignalNotice(20, 7, "item", 1)).toBeUndefined();
  });
  it("replaces feedback with the latest item and action, never a stack", () => {
    let notice = nextSignalNotice(0, 1, "first", 1);
    notice = nextSignalNotice(1, 2, "second", -1);
    expect(notice).toEqual({
      id: 2,
      itemID: "second",
      value: -1,
      text: "Buried. Sizes settle overnight.",
      undo: true,
    });
    notice = nextSignalNotice(2, 3, "second", 0);
    expect(notice).toEqual({
      id: 3,
      itemID: "second",
      value: 0,
      text: "Back to normal.",
      undo: false,
    });
  });
  it("never offers another Undo after returning to neutral", () => {
    expect(nextSignalNotice(1, 2, "boosted-item", 0)?.undo).toBe(false);
    expect(nextSignalNotice(3, 4, "buried-item", 0)?.undo).toBe(false);
  });
});
