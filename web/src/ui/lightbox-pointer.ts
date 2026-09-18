// Remember movement throughout the gesture: a drag returning to its origin
// must never become a scrim click. Pointer capture makes the down target decisive.
export function lightboxTap(
  gesture: {
    x: number;
    y: number;
    moved: boolean;
    scrim: boolean;
    pinched?: boolean;
  },
  x: number,
  y: number,
): "scrim" | "image" | undefined {
  if (
    gesture.moved ||
    gesture.pinched ||
    Math.hypot(x - gesture.x, y - gesture.y) >= 8
  )
    return;
  return gesture.scrim ? "scrim" : "image";
}
