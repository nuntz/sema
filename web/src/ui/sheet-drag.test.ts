import { describe, expect, it } from "vitest";
import {
  beginSheetDrag,
  sheetDragOffset,
  shouldDismissSheet,
} from "./sheet-drag";

const touchDrag = () => {
  const gesture = beginSheetDrag("touch", 100, 0, 0);
  if (!gesture) throw new Error("touch drag should start");
  return gesture;
};

describe("bottom-sheet drag", () => {
  it("dismisses past thirty percent of the sheet height", () => {
    expect(shouldDismissSheet(touchDrag(), 220, 1_000, 400)).toBe(true);
  });

  it("dismisses a short, fast downward flick", () => {
    expect(shouldDismissSheet(touchDrag(), 140, 60, 400)).toBe(true);
  });

  it("rubber-bands upward movement and caps it at 24px", () => {
    expect(sheetDragOffset(touchDrag(), 50)).toBe(-10);
    expect(sheetDragOffset(touchDrag(), -100)).toBe(-24);
  });

  it("ignores non-touch pointers and content that is already scrolled", () => {
    expect(beginSheetDrag("mouse", 100, 0, 0)).toBeUndefined();
    expect(beginSheetDrag("touch", 100, 0, 1)).toBeUndefined();
  });
});
