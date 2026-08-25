import type { Item } from "./types";

export interface ResponsiveMediaSource {
  src?: string;
  srcset?: string;
  sizes?: string;
}

export function responsiveMediaSource(
  item: Pick<Item, "media_url" | "media_variants">,
  renderedSize?: number | string,
): ResponsiveMediaSource {
  const variants = [...(item.media_variants ?? [])]
    .filter((variant) => variant.url && variant.width > 0)
    .sort((left, right) => left.width - right.width)
    .filter(
      (variant, index, values) =>
        index === 0 || variant.width !== values[index - 1].width,
    );
  if (variants.length === 0) return { src: item.media_url };
  return {
    src: item.media_url,
    srcset: variants
      .map((variant) => `${variant.url} ${variant.width}w`)
      .join(", "),
    sizes:
      typeof renderedSize === "number"
        ? `${Math.ceil(renderedSize)}px`
        : renderedSize,
  };
}
