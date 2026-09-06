export const SHEET_DISMISS_DISTANCE = 0.3;
export const SHEET_DISMISS_VELOCITY = 0.6;
export const SHEET_UPWARD_RESISTANCE = 0.2;
export const SHEET_UPWARD_CAP = 24;

export interface SheetDragGesture {
  startY: number;
  startedAt: number;
}

export function beginSheetDrag(
  pointerType: string,
  y: number,
  startedAt: number,
  scrollTop: number,
): SheetDragGesture | undefined {
  if (pointerType !== "touch" || scrollTop > 0) return;
  return { startY: y, startedAt };
}

export function sheetDragOffset(gesture: SheetDragGesture, y: number): number {
  const distance = y - gesture.startY;
  if (distance >= 0) return distance;
  return Math.max(-SHEET_UPWARD_CAP, distance * SHEET_UPWARD_RESISTANCE);
}

export function shouldDismissSheet(
  gesture: SheetDragGesture,
  y: number,
  endedAt: number,
  sheetHeight: number,
): boolean {
  const distance = y - gesture.startY;
  if (distance <= 0) return false;
  const elapsed = Math.max(1, endedAt - gesture.startedAt);
  return (
    distance >= sheetHeight * SHEET_DISMISS_DISTANCE ||
    distance / elapsed > SHEET_DISMISS_VELOCITY
  );
}

export function sheetScrimOpacity(offset: number, sheetHeight: number): number {
  if (sheetHeight <= 0) return 1;
  return Math.max(0, Math.min(1, 1 - Math.max(0, offset) / sheetHeight));
}
