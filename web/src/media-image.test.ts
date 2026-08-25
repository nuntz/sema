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
