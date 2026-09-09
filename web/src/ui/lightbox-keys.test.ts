import { expect, it } from "vitest";
import { imageIndex, lightboxCommand } from "./lightbox-keys";

it("maps image navigation and never wraps at either end", () => {
  expect(lightboxCommand("j")).toBe("next");
  expect(lightboxCommand("k")).toBe("previous");
  expect(lightboxCommand("Escape")).toBe("close");
  expect(imageIndex(0, -1, 3)).toBe(0);
  expect(imageIndex(2, 1, 3)).toBe(2);
  expect(imageIndex(1, 1, 3)).toBe(2);
});
it("swallows reader actions", () => {
  for (const key of [
    "n",
    "p",
    " ",
    "PageUp",
    "PageDown",
    "v",
    "+",
    "-",
    "f",
    "r",
    "c",
  ])
    expect(lightboxCommand(key)).toBe("swallow");
});
