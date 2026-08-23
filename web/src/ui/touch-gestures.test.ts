import { describe, expect, it } from "vitest";
import {
  beginLongPress,
  beginSwipe,
  LONG_PRESS_MS,
  lockSwipeAxis,
  longPressReady,
  moveLongPress,
  swipeCommand,
  swipeOffset,
} from "./touch-gestures";

describe("reader swipe", () => {
  it("axis-locks after 10px and lets vertical scroll win", () => {
    const gesture = beginSwipe(100, 100, 0, 390);
    expect(lockSwipeAxis(gesture, 105, 106)).toBe("pending");
    expect(lockSwipeAxis(gesture, 106, 114)).toBe("vertical");
    expect(swipeCommand(gesture, 20, 100)).toBeUndefined();
  });

  it("requires distance and velocity", () => {
    const fast = beginSwipe(200, 100, 0, 390);
    lockSwipeAxis(fast, 185, 101);
    expect(swipeCommand(fast, 130, 200)).toBe("next");

    const short = beginSwipe(200, 100, 0, 390);
    lockSwipeAxis(short, 185, 100);
    expect(swipeCommand(short, 150, 100)).toBeUndefined();

    const slow = beginSwipe(200, 100, 0, 390);
    lockSwipeAxis(slow, 185, 100);
    expect(swipeCommand(slow, 130, 1_000)).toBeUndefined();
  });

  it("keeps browser edge swipes and rubber-bands unavailable directions", () => {
    const edge = beginSwipe(20, 100, 0, 390);
    expect(edge.eligible).toBe(false);
    const gesture = beginSwipe(100, 100, 0, 390);
    lockSwipeAxis(gesture, 120, 100);
    expect(swipeOffset(gesture, 180, false, true)).toBeLessThan(20);
    expect(swipeOffset(gesture, 180, true, true)).toBeGreaterThan(30);
  });
});

describe("cell long press", () => {
  it("opens at the threshold without movement", () => {
    const gesture = beginLongPress(20, 30, 100);
    expect(longPressReady(gesture, 100 + LONG_PRESS_MS - 1)).toBe(false);
    expect(longPressReady(gesture, 100 + LONG_PRESS_MS)).toBe(true);
  });

  it("is cancelled by scroll-sized movement", () => {
    const gesture = beginLongPress(20, 30, 0);
    expect(moveLongPress(gesture, 24, 34)).toBe(false);
    expect(moveLongPress(gesture, 20, 42)).toBe(true);
    expect(longPressReady(gesture, LONG_PRESS_MS + 20)).toBe(false);
  });
});
