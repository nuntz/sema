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

// Lightbox travel is absolute and upward motion is inert, unlike the sheet.
export function lightboxDragOffset(
  gesture: SheetDragGesture,
  y: number,
): number {
  const offset = Math.max(0, y - gesture.startY);
  return offset > 8 ? offset : 0;
}

export function shouldDismissLightbox(
  gesture: SheetDragGesture,
  y: number,
  endedAt: number,
): boolean {
  const distance = y - gesture.startY;
  return (
    distance > 0 &&
    (distance >= 120 ||
      distance / Math.max(1, endedAt - gesture.startedAt) >= 0.5)
  );
}

export function lightboxDragStyle(distance: number) {
  const d = Math.max(0, Math.min(120, distance));
  const interpolate = (a: number, b: number, t: number) => a + (b - a) * t;
  return {
    scale:
      d <= 40
        ? interpolate(1, 0.97, d / 40)
        : interpolate(0.97, 0.86, (d - 40) / 80),
    opacity:
      d <= 8
        ? interpolate(0.94, 0.88, d / 8)
        : d <= 40
          ? interpolate(0.88, 0.78, (d - 8) / 32)
          : interpolate(0.78, 0.52, (d - 40) / 80),
    radius:
      d <= 40 ? interpolate(0, 6, d / 40) : interpolate(6, 10, (d - 40) / 80),
  };
}
