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
  element.innerHTML = markup;
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
