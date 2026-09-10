import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  buildLightboxSet,
  fittedRect,
  type LightboxImage,
  originalSource,
} from "./lightbox-set";

beforeEach(() => {
  vi.stubGlobal("document", {
    baseURI: "https://sema.test/reader",
    location: { origin: "https://sema.test" },
  });
});
afterEach(() => vi.unstubAllGlobals());

function elementNode(selector: string, childNodes: unknown[] = []) {
  return {
    nodeType: 1,
    childNodes,
    matches: (selectors: string) => selectors.split(", ").includes(selector),
  };
}

function member(
  src = "https://sema.test/media/a.jpg",
  options: {
    width?: number;
    height?: number;
    rendered?: number;
    excluded?: boolean;
    ancestors?: string[];
    caption?: string;
    naturalWidth?: number;
    naturalHeight?: number;
    anchor?: {
      href: string;
      imageOnly?: boolean;
      descendant?: string;
      injected?: boolean;
    };
  } = {},
): LightboxImage {
  const link = options.anchor;
  const imageNode = elementNode("img");
  const anchor = link
    ? {
        getAttribute: () => link.href,
        childNodes: [
          link.injected
            ? elementNode("span.lb-inline", [
                imageNode,
                elementNode("span.lb-hover-pill", [
                  elementNode("svg"),
                  { nodeType: 3, textContent: "1 / 3" },
                ]),
              ])
            : imageNode,
          {
            nodeType: 3,
            textContent: link.imageOnly === false ? "Go to image" : " \n ",
          },
          ...(link.descendant ? [elementNode(link.descendant)] : []),
        ],
      }
    : null;
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
        : selector === "a"
          ? anchor
          : options.excluded ||
              options.ancestors?.some((ancestor) =>
                selector
                  .split(", ")
                  .some(
                    (candidate) =>
                      candidate === ancestor ||
                      (candidate.startsWith(".") &&
                        ancestor.endsWith(candidate)),
                  ),
              )
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
  it("keeps same-origin cached images, including relative sources", () => {
    expect(buildLightboxSet(body(member("/media/body-0.webp")))[0].src).toBe(
      "https://sema.test/media/body-0.webp",
    );
    expect(buildLightboxSet(body(member()))).toHaveLength(1);
  });
  it("allows a stored image in a block Reddit card and lead host", () => {
    const image = member("/media/reddit.jpg", {
      ancestors: ["span.lb-lead-host", "div.reddit-media-card"],
    });
    expect(buildLightboxSet(body(image))).toHaveLength(1);
    expect(buildLightboxSet(null, image)).toHaveLength(1);
  });
  it("excludes an image in an anchor Reddit card", () => {
    const image = member("/media/reddit.jpg", {
      ancestors: ["a.reddit-media-card"],
      anchor: { href: "https://i.redd.it/image.jpg" },
    });
    expect(buildLightboxSet(body(image))).toEqual([]);
    expect(buildLightboxSet(null, image)).toEqual([]);
  });
  it("keeps image-only links to image-file pathnames without changing the cached source", () => {
    for (const href of [
      "https://cdnb.artstation.com/artwork.JPG?1788861996",
      "/original.jpeg",
      "/original.png#view",
      "/original.gif",
      "/original.webp",
      "/original.avif",
      "/original.SVG",
    ]) {
      const image = member(undefined, { anchor: { href } });
      const result = buildLightboxSet(body(image));
      expect(result).toHaveLength(1);
      expect(result[0].src).toBe(image.src);
      expect(originalSource(result[0])).toBe(image.src);
    }
  });
  it("keeps archived lead and body images with their archived sources and variants", () => {
    const lead = {
      ...member("https://sema.test/archive/user/item/lead.jpg"),
      variants: [
        {
          url: "/archive/user/item/lead-full.jpg",
          width: 2400,
          height: 1200,
        },
      ],
    };
    const result = buildLightboxSet(
      body(member("/archive/user/item/body-0.webp")),
      lead,
    );
    expect(result.map((image) => image.src)).toEqual([
      lead.src,
      "https://sema.test/archive/user/item/body-0.webp",
    ]);
    expect(originalSource(result[0])).toBe(lead.variants[0].url);
    expect(originalSource(result[1])).toBe(result[1].src);
  });
  it("rejects page links, text links, unsupported descendants, and malformed hrefs", () => {
    for (const anchor of [
      { href: "https://www.artstation.com/artwork/zzPkW2" },
      { href: "/page.html?image=photo.jpg" },
      { href: "/image.jpg", imageOnly: false },
      { href: "/image.jpg", descendant: "button" },
      { href: "/image.jpg", descendant: "span" },
      { href: "http://[invalid/image.jpg" },
    ])
      expect(buildLightboxSet(body(member(undefined, { anchor })))).toEqual([]);
  });
  it("allows picture/source and reader-injected wrappers and pills during rebuilds", () => {
    for (const descendant of ["picture", "source"])
      expect(
        buildLightboxSet(
          body(
            member(undefined, {
              anchor: { href: "/image.jpg", injected: true, descendant },
            }),
          ),
        ),
      ).toHaveLength(1);
  });
  it("rejects hotlinked images whether linked or unlinked, and local non-media paths", () => {
    for (const src of [
      "https://cdn.example/photo.jpg",
      "https://cdn.example/media/photo.jpg",
      "https://cdn.example/archive/photo.jpg",
      "https://sema.test/other/photo.jpg",
      "https://sema.test/media-other/photo.jpg",
      "https://sema.test/archive-other/photo.jpg",
    ]) {
      expect(buildLightboxSet(body(member(src)))).toEqual([]);
      expect(
        buildLightboxSet(
          body(
            member(src, { anchor: { href: "https://cdn.example/photo.jpg" } }),
          ),
        ),
      ).toEqual([]);
    }
  });
  it("still excludes media-card anchors with image-file links", () => {
    expect(
      buildLightboxSet(
        body(
          member(undefined, { excluded: true, anchor: { href: "/image.jpg" } }),
        ),
      ),
    ).toEqual([]);
  });
  it("keeps lead then body order, captions and variants", () => {
    const lead = {
      ...member("https://sema.test/media/lead.jpg"),
      variants: [
        { url: "https://sema.test/media/full.jpg", width: 2000, height: 1000 },
      ],
    };
    const first = member();
    const second = member("https://sema.test/media/b.jpg");
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
  it("excludes media images, small declared or rendered images and non-cached sources", () => {
    expect(
      buildLightboxSet(
        body(
          member("https://sema.test/media/link", { excluded: true }),
          member("https://sema.test/media/small", { width: 199, height: 10 }),
          member("https://sema.test/media/rendered", { rendered: 199 }),
          member("data:image/png;base64,abc"),
          member("javascript:alert(1)"),
        ),
      ),
    ).toEqual([]);
    expect(
      buildLightboxSet(
        body(member("https://sema.test/media/boundary", { width: 200 })),
      ),
    ).toHaveLength(1);
  });
  it("keeps images whose unloaded inline placeholder is smaller than 200px", () => {
    const image = member("https://sema.test/media/later.jpg", {
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
      ...member("https://sema.test/media/a", { width: 2400, height: 1200 }),
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
