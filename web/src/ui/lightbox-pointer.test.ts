import { expect, it } from "vitest";
import { lightboxTap } from "./lightbox-pointer";

it("recognizes a scrim click using the same tap threshold for every pointer and zoom", () => {
  const scrim = { x: 100, y: 200, scrim: true, moved: false };
  expect(lightboxTap(scrim, 100, 200)).toBe("scrim");
  expect(lightboxTap(scrim, 104, 204)).toBe("scrim");
  expect(lightboxTap(scrim, 108, 200)).toBeUndefined();
  expect(lightboxTap({ ...scrim, moved: true }, 100, 200)).toBeUndefined();
  expect(lightboxTap({ ...scrim, pinched: true }, 100, 200)).toBeUndefined();
  expect(lightboxTap({ ...scrim, scrim: false }, 100, 200)).toBe("image");
});
