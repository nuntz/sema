import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Item } from "../types";
import { gridLightboxLead, loadGridLightboxImages } from "./grid-lightbox";

const item = {
  media_url: "https://sema.test/media/lead.jpg",
  media_w: 800,
  media_h: 600,
  media_variants: [{ url: "/media/large.jpg", width: 1600, height: 1200 }],
  has_body: true,
  body_url: "/archive/body.html",
} as Item;

function image(src: string) {
  return {
    src,
    alt: "",
    closest: () => null,
    getAttribute: () => null,
    getBoundingClientRect: () => ({ width: 0, height: 0 }),
  } as unknown as HTMLImageElement;
}

beforeEach(() => {
  // Minimal detached DOM for the node test environment; browser tests exercise parsing.
  vi.stubGlobal("document", {
    baseURI: "https://sema.test/",
    location: { origin: "https://sema.test" },
    createElement: () => ({
      innerHTML: "",
      querySelectorAll() {
        return Array.from(this.innerHTML.matchAll(/src="([^"]+)"/g), (match) =>
          image(match[1]),
        );
      },
    }),
  });
});
afterEach(() => vi.unstubAllGlobals());

it("builds the stored lead with dimensions and variants", () => {
  const element = image(item.media_url ?? "");
  expect(gridLightboxLead(item, element)).toEqual({
    element,
    src: item.media_url,
    width: 800,
    height: 600,
    variants: item.media_variants,
    alt: "",
    caption: "",
  });
  for (const change of [
    { media_url: undefined },
    { media_type: "video" },
    { post_type: "video" },
  ])
    expect(
      gridLightboxLead({ ...item, ...change } as Item, element),
    ).toBeUndefined();
});

it("loads body images in order, filtering external images and duplicates", async () => {
  const fetcher = vi
    .fn()
    .mockImplementation(
      () =>
        new Response(
          '<img src="/media/lead.jpg"><img src="/media/a.jpg"><img src="https://other.test/b.jpg"><img src="/media/b.jpg">',
        ),
    );
  vi.stubGlobal("fetch", fetcher);
  const signal = new AbortController().signal;
  const lead = gridLightboxLead(item, image(item.media_url ?? ""));
  const images = await loadGridLightboxImages(item, lead, signal);
  expect(images.map((member) => member.src)).toEqual([
    item.media_url,
    "https://sema.test/media/a.jpg",
    "https://sema.test/media/b.jpg",
  ]);
  expect(images.slice(1).map((member) => member.element.loading)).toEqual([
    "eager",
    "eager",
  ]);
  expect(fetcher).toHaveBeenCalledWith(item.body_url, {
    credentials: "same-origin",
    signal,
  });
  expect(await loadGridLightboxImages(item, undefined, signal)).toHaveLength(3);
});

it("silently retains the lead after failed or aborted fetches", async () => {
  const lead = gridLightboxLead(item, image(item.media_url ?? ""));
  const controller = new AbortController();
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
  expect(await loadGridLightboxImages(item, lead, controller.signal)).toEqual([
    lead,
  ]);
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response("", { status: 500 })),
  );
  expect(await loadGridLightboxImages(item, lead, controller.signal)).toEqual([
    lead,
  ]);
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(() => {
      controller.abort();
      return Promise.resolve(new Response('<img src="/media/late.jpg">'));
    }),
  );
  expect(await loadGridLightboxImages(item, lead, controller.signal)).toEqual([
    lead,
  ]);
  expect(await loadGridLightboxImages(item, lead, controller.signal)).toEqual([
    lead,
  ]);
});

it("keeps the lead when its grid thumbnail is smaller than the reader cutoff", async () => {
  const element = image(item.media_url ?? "");
  Object.defineProperty(element, "naturalWidth", { value: 800 });
  element.getBoundingClientRect = () =>
    ({ width: 150, height: 100 }) as DOMRect;
  const lead = gridLightboxLead(item, element);
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response('<img src="/media/lead.jpg"><img src="/media/body.jpg">'),
      ),
  );
  const images = await loadGridLightboxImages(
    item,
    lead,
    new AbortController().signal,
  );
  expect(images).toHaveLength(2);
  expect(images[0]).toBe(lead);
  expect(images[1].src).toBe("https://sema.test/media/body.jpg");
});
