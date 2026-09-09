import { describe, expect, it } from "vitest";
import {
  buildLightboxSet,
  fittedRect,
  type LightboxImage,
  originalSource,
} from "./lightbox-set";

function member(
  src = "https://example.com/a.jpg",
  options: {
    width?: number;
    height?: number;
    rendered?: number;
    excluded?: boolean;
    caption?: string;
    naturalWidth?: number;
    naturalHeight?: number;
  } = {},
): LightboxImage {
  const element = {
    src,
    alt: "Description",
    naturalWidth: options.naturalWidth ?? 800,
    naturalHeight: options.naturalHeight ?? 400,
    getAttribute: (key: string) =>
      key === "width" ? options.width : options.height,
    getBoundingClientRect: () => ({
      width: options.rendered ?? 400,
      height: 100,
    }),
    closest: (selector: string) =>
      selector === "figure"
        ? {
            querySelector: () => ({
              textContent: options.caption ?? " Caption ",
            }),
          }
        : options.excluded
          ? {}
          : null,
  } as unknown as HTMLImageElement;
  return {
    element,
    src,
    alt: element.alt,
    caption: "",
    width: options.width,
    height: options.height,
  };
}
const body = (...images: LightboxImage[]) =>
  ({
    querySelectorAll: () => images.map((image) => image.element),
  }) as unknown as ParentNode;

describe("lightbox set", () => {
  it("keeps lead then body order, captions and variants", () => {
    const lead = {
      ...member("https://example.com/lead.jpg"),
      variants: [
        { url: "https://example.com/full.jpg", width: 2000, height: 1000 },
      ],
    };
    const first = member();
    const second = member("https://example.com/b.jpg");
    const result = buildLightboxSet(body(first, second), lead);
    expect(result.map((image) => image.src)).toEqual([
      lead.src,
      first.src,
      second.src,
    ]);
    expect(result[1].caption).toBe("Caption");
    expect(result[0].variants).toBe(lead.variants);
  });
  it("deduplicates lead and body sources", () => {
    const lead = member();
    expect(buildLightboxSet(body(member(), member()), lead)).toEqual([lead]);
  });
  it("excludes linked/media images, small declared or rendered images and non-http sources", () => {
    expect(
      buildLightboxSet(
        body(
          member("https://example.com/link", { excluded: true }),
          member("https://example.com/small", { width: 199, height: 10 }),
          member("https://example.com/rendered", { rendered: 199 }),
          member("data:image/png;base64,abc"),
          member("javascript:alert(1)"),
        ),
      ),
    ).toEqual([]);
    expect(
      buildLightboxSet(
        body(member("https://example.com/boundary", { width: 200 })),
      ),
    ).toHaveLength(1);
  });
  it("keeps images whose unloaded inline placeholder is smaller than 200px", () => {
    const image = member("https://example.com/later.jpg", {
      rendered: 32,
      naturalWidth: 0,
      naturalHeight: 0,
    });
    Object.assign(image.element, { complete: false });
    expect(buildLightboxSet(body(image))).toHaveLength(1);
  });
  it("keeps a broken full-size image even when its alt-text fallback is small", () => {
    const image = member(undefined, {
      width: 1600,
      height: 1000,
      rendered: 100,
      naturalWidth: 0,
    });
    Object.assign(image.element, { complete: true });
    expect(buildLightboxSet(body(image))).toHaveLength(1);
  });
  it("handles an empty body and unknown dimensions", () => {
    expect(buildLightboxSet(null)).toEqual([]);
    expect(buildLightboxSet(body(member()))).toHaveLength(1);
  });
});

describe("fixed image geometry", () => {
  it("fits the lead from stored dimensions and picks the largest variant", () => {
    const lead = {
      ...member("https://example.com/a", { width: 2400, height: 1200 }),
      variants: [
        { url: "large", width: 2400, height: 1200 },
        { url: "small", width: 640, height: 320 },
      ],
    };
    expect(fittedRect(lead, 1280, 900)).toEqual({
      width: expect.closeTo(1240),
      height: expect.closeTo(620),
      left: expect.closeTo(20),
      top: expect.closeTo(140),
    });
    expect(originalSource(lead)).toBe("large");
  });
  it("uses declared body dimensions before natural dimensions", () => {
    expect(
      fittedRect(member(undefined, { width: 400, height: 800 }), 390, 844),
    ).toEqual({ width: 350, height: 700, left: 20, top: 72 });
  });
  it("uses decoded body dimensions and preserves a captured rect after late decode", () => {
    const image = member();
    expect(fittedRect(image, 390, 844)).toEqual({
      width: 350,
      height: 175,
      left: 20,
      top: 334.5,
    });
    const broken = member(undefined, { naturalWidth: 0, naturalHeight: 0 });
    const rect = fittedRect(broken, 390, 844);
    Object.assign(broken.element, { naturalWidth: 100, naturalHeight: 900 });
    expect(rect).toEqual({ width: 350, height: 87.5, left: 20, top: 378.25 });
    expect(originalSource(broken)).toBe(broken.src);
  });
});
