import { describe, expect, it } from "vitest";
import { linkBehaviourEvent, mergeBehaviourEvent } from "./behaviour-events";

describe("behaviour event batching", () => {
  it("keeps dwell and flags monotonic across a debounce window", () => {
    const openedLater = mergeBehaviourEvent(
      { dwell_ms: 31_000, clicked_through: true },
      { dwell_ms: 12_000, shared: true },
    );
    expect(openedLater).toEqual({
      dwell_ms: 31_000,
      clicked_through: true,
      shared: true,
    });
  });
});

describe("link behaviour", () => {
  it("marks both copied and natively shared links as shared", () => {
    expect(linkBehaviourEvent()).toEqual({
      clicked_through: true,
      shared: true,
    });
  });
});
