import { describe, expect, it } from "vitest";
import { gridImageLookahead } from "./image-loading";

describe("gridImageLookahead", () => {
  it.each([
    [0, 0, 0],
    [400, 100, 500],
    [844, 211, 1055],
    [960, 240, 1200],
    [1200, 240, 1440],
  ])(
    "uses one page plus a capped buffer for height %i",
    (height, top, bottom) => {
      expect(gridImageLookahead(height)).toEqual({ top, bottom });
    },
  );
});
