import type { MediaVariant } from "../types";

export interface LightboxImage {
  element: HTMLImageElement;
  src: string;
  alt: string;
  caption: string;
  width?: number;
  height?: number;
  variants?: MediaVariant[];
}

const excluded =
  "a, .media-card, .video-media-card, .reddit-media-card, video, iframe, .video-wrapper, .iframe-wrapper, .video-container, .video-embed, .iframe-embed, .embed-container, .embed-responsive, .embed, [data-video]";

/** Reads the injected DOM without mutating it. Unknown dimensions remain eligible. */
export function buildLightboxSet(
  body: ParentNode | null,
  lead?: LightboxImage,
): LightboxImage[] {
  const candidates = [
    ...(lead ? [lead] : []),
    ...Array.from(body?.querySelectorAll<HTMLImageElement>("img") ?? []).map(
      (element) => ({
        element,
        src: element.src,
        alt: element.alt,
        caption:
          element
            .closest("figure")
            ?.querySelector("figcaption")
            ?.textContent?.trim() ?? "",
        width: Number(element.getAttribute("width")) || undefined,
        height: Number(element.getAttribute("height")) || undefined,
      }),
    ),
  ];
  const seen = new Set<string>();
  return candidates.filter((member) => {
    const { element, src, width, height } = member;
    if (
      !/^https?:\/\//i.test(src) ||
      element.closest(excluded) ||
      seen.has(src)
    )
      return false;
    const rect = element.getBoundingClientRect();
    // Before pixels load (or after failure), this may only be the tiny alt-text
    // placeholder. It says nothing about the image's eventual rendered size.
    const rendered = element.naturalWidth
      ? Math.max(rect.width, rect.height)
      : 0;
    const declared = Math.max(width ?? 0, height ?? 0);
    if ((rendered > 0 && rendered < 200) || (declared > 0 && declared < 200))
      return false;
    seen.add(src);
    return true;
  });
}

export function imageDimensions(image: LightboxImage) {
  if (image.width && image.height)
    return { width: image.width, height: image.height };
  const element = image.element;
  if (element.naturalWidth && element.naturalHeight)
    return { width: element.naturalWidth, height: element.naturalHeight };
  const rect = element.getBoundingClientRect();
  return { width: rect.width || 640, height: rect.height || 360 };
}

export function fittedRect(
  image: LightboxImage,
  viewportWidth: number,
  viewportHeight: number,
) {
  const size = imageDimensions(image);
  const scale = Math.min(
    (viewportWidth - 40) / size.width,
    (viewportHeight - 40) / size.height,
  );
  const width = size.width * scale;
  const height = size.height * scale;
  return {
    width,
    height,
    left: (viewportWidth - width) / 2,
    top: (viewportHeight - height) / 2,
  };
}

export function originalSource(image: LightboxImage) {
  return (
    image.variants?.reduce<MediaVariant | undefined>(
      (largest, variant) =>
        !largest ||
        variant.width * variant.height > largest.width * largest.height
          ? variant
          : largest,
      undefined,
    )?.url ?? image.src
  );
}
