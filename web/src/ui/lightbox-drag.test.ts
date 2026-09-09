import { expect, it } from "vitest";
import {
  beginSheetDrag,
  lightboxDragOffset,
  lightboxDragStyle,
  shouldDismissLightbox,
} from "./sheet-drag";

it("tracks downward travel after 8px and leaves upward motion inert", () => {
  const drag = beginSheetDrag("touch", 100, 0, 0);
  if (!drag) throw new Error("Touch drag should begin");
  expect(lightboxDragOffset(drag, 90)).toBe(0);
  expect(lightboxDragOffset(drag, 108)).toBe(0);
  expect(lightboxDragOffset(drag, 140)).toBe(40);
  expect(lightboxDragStyle(40)).toEqual({
    scale: 0.97,
    opacity: 0.78,
    radius: 6,
  });
  expect(lightboxDragStyle(120)).toEqual({
    scale: 0.86,
    opacity: 0.52,
    radius: 10,
  });
});
it("dismisses at 120px or 0.5px/ms, never upward", () => {
  const drag = beginSheetDrag("touch", 100, 0, 0);
  if (!drag) throw new Error("Touch drag should begin");
  expect(shouldDismissLightbox(drag, 219, 1000)).toBe(false);
  expect(shouldDismissLightbox(drag, 220, 1000)).toBe(true);
  expect(shouldDismissLightbox(drag, 150, 100)).toBe(true);
  expect(shouldDismissLightbox(drag, 90, 1)).toBe(false);
});
