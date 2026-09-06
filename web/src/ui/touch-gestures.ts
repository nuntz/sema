export type SwipeAxis = "pending" | "horizontal" | "vertical";
export const LONG_PRESS_MS = 420;

export interface SwipeGesture {
  startX: number;
  startY: number;
  startedAt: number;
  axis: SwipeAxis;
  eligible: boolean;
}

export interface LongPressGesture {
  startX: number;
  startY: number;
  startedAt: number;
  cancelled: boolean;
}

export function beginLongPress(
  x: number,
  y: number,
  startedAt: number,
): LongPressGesture {
  return { startX: x, startY: y, startedAt, cancelled: false };
}

export function moveLongPress(
  gesture: LongPressGesture,
  x: number,
  y: number,
): boolean {
  if (Math.hypot(x - gesture.startX, y - gesture.startY) > 8)
    gesture.cancelled = true;
  return gesture.cancelled;
}

export function longPressReady(
  gesture: LongPressGesture,
  now: number,
): boolean {
  return !gesture.cancelled && now - gesture.startedAt >= LONG_PRESS_MS;
}

export function beginSwipe(
  x: number,
  y: number,
  startedAt: number,
  viewportWidth: number,
  standalone = false,
): SwipeGesture {
  return {
    startX: x,
    startY: y,
    startedAt,
    axis: "pending",
    eligible: (standalone || x >= 24) && x <= viewportWidth - 24,
  };
}

export function lockSwipeAxis(
  gesture: SwipeGesture,
  x: number,
  y: number,
): SwipeAxis {
  if (!gesture.eligible || gesture.axis !== "pending") return gesture.axis;
  const dx = x - gesture.startX;
  const dy = y - gesture.startY;
  if (Math.hypot(dx, dy) < 10) return "pending";
  gesture.axis = Math.abs(dx) > Math.abs(dy) ? "horizontal" : "vertical";
  return gesture.axis;
}

export function swipeCommand(
  gesture: SwipeGesture,
  x: number,
  endedAt: number,
): "next" | undefined {
  if (!gesture.eligible || gesture.axis !== "horizontal") return;
  const dx = x - gesture.startX;
  const elapsed = Math.max(1, endedAt - gesture.startedAt);
  if (dx > -60 || -dx / elapsed < 0.2) return;
  return "next";
}

export function swipeOffset(
  gesture: SwipeGesture,
  x: number,
  canNext: boolean,
): number {
  if (gesture.axis !== "horizontal") return 0;
  const dx = x - gesture.startX;
  if (dx >= 0) return 0;
  return canNext ? dx : Math.max(-96, dx * 0.16);
}

export function panelOffset(
  gesture: SwipeGesture,
  x: number,
  viewportWidth: number,
): number {
  if (!gesture.eligible || gesture.axis !== "horizontal") return 0;
  return Math.max(0, Math.min(viewportWidth, x - gesture.startX));
}

export function closeCommand(
  gesture: SwipeGesture,
  x: number,
  endedAt: number,
  viewportWidth: number,
): "close" | undefined {
  if (!gesture.eligible || gesture.axis !== "horizontal") return;
  const distance = x - gesture.startX;
  if (distance <= 0) return;
  const elapsed = Math.max(1, endedAt - gesture.startedAt);
  if (distance >= viewportWidth * 0.35 || distance / elapsed > 0.5)
    return "close";
}
