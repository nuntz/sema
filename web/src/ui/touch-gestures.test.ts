import { describe, expect, it } from "vitest";
import {
  beginLongPress,
  beginSwipe,
  closeCommand,
  LONG_PRESS_MS,
  lockSwipeAxis,
  longPressReady,
  moveLongPress,
  panelOffset,
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
    expect(panelOffset(gesture, 180, 390)).toBe(80);

    const unavailable = beginSwipe(100, 100, 0, 390);
    lockSwipeAxis(unavailable, 80, 100);
    expect(swipeOffset(unavailable, 0, false)).toBe(-16);
    expect(swipeOffset(unavailable, 0, true)).toBe(-100);
  });

  it("uses right swipes to close rather than move to the previous item", () => {
    const gesture = beginSwipe(100, 100, 0, 390);
    lockSwipeAxis(gesture, 120, 100);
    expect(swipeCommand(gesture, 300, 200)).toBeUndefined();
    expect(closeCommand(gesture, 300, 200, 390)).toBe("close");
  });

  it("snaps back below the close threshold", () => {
    const gesture = beginSwipe(100, 100, 0, 400);
    lockSwipeAxis(gesture, 112, 100);
    expect(closeCommand(gesture, 220, 1_000, 400)).toBeUndefined();
  });

  it("commits close by distance or fast rightward velocity", () => {
    const far = beginSwipe(100, 100, 0, 400);
    lockSwipeAxis(far, 112, 100);
    expect(closeCommand(far, 240, 1_000, 400)).toBe("close");

    const fast = beginSwipe(100, 100, 0, 400);
    lockSwipeAxis(fast, 112, 100);
    expect(closeCommand(fast, 140, 60, 400)).toBe("close");
  });

  it("reserves the left edge in tabs but accepts it in standalone mode", () => {
    expect(beginSwipe(20, 100, 0, 390).eligible).toBe(false);
    expect(beginSwipe(20, 100, 0, 390, true).eligible).toBe(true);
    expect(beginSwipe(380, 100, 0, 390, true).eligible).toBe(false);
  });

  it("does not close after the gesture locks vertically", () => {
    const gesture = beginSwipe(100, 100, 0, 390);
    expect(lockSwipeAxis(gesture, 104, 115)).toBe("vertical");
    expect(closeCommand(gesture, 300, 100, 390)).toBeUndefined();
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
