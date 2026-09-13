import type { Item } from "../types";
import { buildLightboxSet, type LightboxImage } from "./lightbox-set";

export function gridLightboxLead(
  item: Item,
  element: HTMLImageElement,
): LightboxImage | undefined {
  if (
    !item.media_url ||
    item.media_type === "video" ||
    item.post_type === "video"
  )
    return;
  return {
    element,
    src: item.media_url,
    alt: "",
    caption: "",
    width: item.media_w,
    height: item.media_h,
    variants: item.media_variants ?? [],
  };
}

export async function loadGridLightboxImages(
  item: Item,
  lead: LightboxImage | undefined,
  signal: AbortSignal,
): Promise<LightboxImage[]> {
  const fallback = lead ? [lead] : [];
  if (!item.has_body || !item.body_url || signal.aborted) return fallback;
  try {
    const response = await fetch(item.body_url, {
      credentials: "same-origin",
      signal,
    });
    if (!response.ok) return fallback;
    const markup = await response.text();
    if (signal.aborted) return fallback;
    const container = document.createElement("div");
    container.className = "article-body";
    container.innerHTML = markup;
    const images = buildLightboxSet(container, lead);
    // Archived images are lazy by default. Detached nodes never intersect the
    // viewport, but the lightbox needs their decoded pixels and natural size.
    for (const image of images) {
      if (image.element !== lead?.element) image.element.loading = "eager";
    }
    if (!lead) return images;
    // Grid thumbnails can render below the reader's 200px cutoff. Keep the
    // already-open lead and its origin even when that geometry filters it out.
    const leadURL = new URL(lead.src, document.baseURI).href;
    return [lead, ...images.filter((image) => image.src !== leadURL)];
  } catch {
    return fallback;
  }
}
