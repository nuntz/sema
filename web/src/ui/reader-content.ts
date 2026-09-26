import { decodeImageWithin } from "../image-decode";

const LEADING_IMAGE = /^\s*(?:(?:<p|<a|<figure)(?:\s[^>]*)?>\s*)*<img(?:\s|>)/i;

export function hasLeadingImage(markup: string): boolean {
  return LEADING_IMAGE.test(markup);
}

export interface PreparedReaderBody {
  markup: string;
  element: HTMLDivElement;
  leadingImage: boolean;
}

export async function prepareReaderBody(
  markup: string,
): Promise<PreparedReaderBody> {
  const element = document.createElement("div");
  element.className = "article-body";
  // Template content is inert: reject remote thumbnails before any image can load.
  const template = document.createElement("template");
  template.innerHTML = markup;
  for (const source of template.content.querySelectorAll(".media-card source"))
    source.remove();
  for (const image of template.content.querySelectorAll<HTMLImageElement>(
    ".media-card img",
  )) {
    if (!firstPartyThumbnail(image)) image.remove();
  }
  element.append(template.content);
  const leadingImage = hasLeadingImage(markup);
  const image = leadingImage ? element.querySelector("img") : null;
  if (image) {
    // Decode the node that will actually be displayed. Recreating it from HTML
    // after decoding a separate Image can still produce a blank WebKit frame.
    image.loading = "eager";
    image.decoding = "sync";
    // Broken or stalled media must not block the article indefinitely.
    await decodeImageWithin(image, 4_000);
  }
  return { markup, element, leadingImage };
}

export function firstPartyThumbnail(image: HTMLImageElement): boolean {
  const src = image.getAttribute("src");
  if (!src) return false;
  try {
    if (new URL(src, location.href).origin !== location.origin) return false;
  } catch {
    return false;
  }
  image.removeAttribute("srcset");
  image.referrerPolicy = "no-referrer";
  image.loading = "lazy";
  return true;
}
