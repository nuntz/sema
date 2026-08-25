import { createMemo, type JSX, onCleanup, splitProps } from "solid-js";
import { responsiveMediaSource } from "../media-image";
import type { Item } from "../types";

type ResponsiveImageProps = Omit<
  JSX.ImgHTMLAttributes<HTMLImageElement>,
  "src" | "srcset" | "sizes" | "decoding" | "alt"
> & {
  item: Item;
  sizes?: number | string;
  alt: string;
};

export function ResponsiveImage(props: ResponsiveImageProps) {
  const [local, imageProps] = splitProps(props, ["item", "sizes", "alt"]);
  const source = createMemo(() =>
    responsiveMediaSource(local.item, local.sizes),
  );
  let image: HTMLImageElement | undefined;

  onCleanup(() => {
    // WebKit may retain a decoded image after its detached DOM node disappears.
    // Clear the selected source while the element is still owned by this cell.
    image?.removeAttribute("srcset");
    image?.removeAttribute("src");
  });

  return (
    <img
      {...imageProps}
      ref={image}
      src={source().src}
      alt={local.alt}
      srcset={source().srcset}
      sizes={source().sizes}
      loading={imageProps.loading ?? "lazy"}
      decoding="async"
    />
  );
}
