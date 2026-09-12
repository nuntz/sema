import type { Item } from "./types";

export interface ResponsiveMediaSource {
  src?: string;
  srcset?: string;
  sizes?: string;
}

export function responsiveMediaSource(
  item: Pick<Item, "media_url" | "media_variants">,
  renderedSize?: number | string,
  maxDimension?: number,
  pixelRatio?: number,
): ResponsiveMediaSource {
  const available = [...(item.media_variants ?? [])]
    .filter((variant) => variant.url && variant.width > 0)
    .sort((left, right) => left.width - right.width)
    .filter(
      (variant, index, values) =>
        index === 0 || variant.width !== values[index - 1].width,
    );
  if (available.length === 0) return { src: item.media_url };
  // Bound both axes: portrait variants use their height as the encoded size.
  const bounded = maxDimension
    ? available.filter(
        (variant) => Math.max(variant.width, variant.height) <= maxDimension,
      )
    : available;
  // Older items may have only a large variant. Keep their smallest available
  // image usable until smaller variants exist.
  const variants = bounded.length ? bounded : [available[0]];
  if (pixelRatio !== undefined && typeof renderedSize === "number") {
    const targetWidth = Math.ceil(renderedSize) * pixelRatio;
    return {
      src: (
        variants.find((variant) => variant.width >= targetWidth) ??
        variants[variants.length - 1]
      ).url,
    };
  }
  return {
    src: maxDimension ? variants[variants.length - 1].url : item.media_url,
    srcset: variants
      .map((variant) => `${variant.url} ${variant.width}w`)
      .join(", "),
    sizes:
      typeof renderedSize === "number"
        ? `${Math.ceil(renderedSize)}px`
        : renderedSize,
  };
}
