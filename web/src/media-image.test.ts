import { describe, expect, test } from "vitest";
import { responsiveMediaSource } from "./media-image";

describe("responsiveMediaSource", () => {
  test("builds ordered width descriptors and exact rounded grid size", () => {
    expect(
      responsiveMediaSource(
        {
          media_url: "/lead.jpg",
          media_variants: [
            { url: "/lead.jpg", width: 1280, height: 853 },
            { url: "/lead-384.jpg", width: 384, height: 256 },
            { url: "/lead-768.jpg", width: 768, height: 512 },
          ],
        },
        311.2,
      ),
    ).toEqual({
      src: "/lead.jpg",
      srcset: "/lead-384.jpg 384w, /lead-768.jpg 768w, /lead.jpg 1280w",
      sizes: "312px",
    });
  });

  test("uses a caller-provided responsive sizes expression", () => {
    expect(
      responsiveMediaSource(
        {
          media_url: "/lead.jpg",
          media_variants: [{ url: "/lead-384.jpg", width: 384, height: 256 }],
        },
        "(max-width: 700px) calc(100vw - 44px), 640px",
      ).sizes,
    ).toBe("(max-width: 700px) calc(100vw - 44px), 640px");
  });

  test("leaves legacy items on their plain src fallback", () => {
    expect(
      responsiveMediaSource({ media_url: "/legacy-lead.jpg" }, 300),
    ).toEqual({ src: "/legacy-lead.jpg" });
  });
});

describe("grid image size budget", () => {
  test("caps both src and srcset while preserving the real display size", () => {
    expect(
      responsiveMediaSource(
        {
          media_url: "/1280.jpg",
          media_variants: [
            { url: "/1280.jpg", width: 1280, height: 850 },
            { url: "/384.jpg", width: 384, height: 255 },
            { url: "/768.jpg", width: 768, height: 510 },
          ],
        },
        500,
        768,
      ),
    ).toEqual({
      src: "/768.jpg",
      srcset: "/384.jpg 384w, /768.jpg 768w",
      sizes: "500px",
    });
  });

  test("uses the long edge for tall portraits", () => {
    expect(
      responsiveMediaSource(
        {
          media_url: "/portrait-large.jpg",
          media_variants: [
            { url: "/portrait-large.jpg", width: 640, height: 1280 },
            { url: "/portrait-small.jpg", width: 384, height: 768 },
          ],
        },
        400,
        768,
      ).srcset,
    ).toBe("/portrait-small.jpg 384w");
  });

  test("keeps old items usable when no variant fits the budget", () => {
    expect(
      responsiveMediaSource(
        {
          media_url: "/large.jpg",
          media_variants: [
            { url: "/large.jpg", width: 1600, height: 1000 },
            { url: "/smaller.jpg", width: 1280, height: 800 },
          ],
        },
        400,
        768,
      ).src,
    ).toBe("/smaller.jpg");
    expect(
      responsiveMediaSource({ media_url: "/legacy.jpg" }, 400, 768),
    ).toEqual({ src: "/legacy.jpg" });
  });
});

describe("explicit grid source selection", () => {
  const photo = {
    media_url: "/1280.jpg",
    media_variants: [
      { url: "/384.jpg", width: 384, height: 255 },
      { url: "/768.jpg", width: 768, height: 510 },
      { url: "/1280.jpg", width: 1280, height: 850 },
    ],
  };

  test("selects by rendered width and DPR without a native srcset", () => {
    expect(responsiveMediaSource(photo, 300, 768, 1)).toEqual({
      src: "/384.jpg",
    });
    expect(responsiveMediaSource(photo, 300, 768, 2)).toEqual({
      src: "/768.jpg",
    });
    expect(responsiveMediaSource(photo, 800, 768, 2)).toEqual({
      src: "/768.jpg",
    });
  });

  test("keeps native responsive selection for non-grid sizes expressions", () => {
    expect(responsiveMediaSource(photo, "640px").srcset).toContain("1280w");
  });
});
