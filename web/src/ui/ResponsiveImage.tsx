import { createMemo, type JSX, onCleanup, onMount, splitProps } from "solid-js";
import { decodeImageWithin } from "../image-decode";
import { responsiveMediaSource } from "../media-image";
import type { Item } from "../types";

type ResponsiveImageProps = Omit<
  JSX.ImgHTMLAttributes<HTMLImageElement>,
  "src" | "srcset" | "sizes" | "alt"
> & {
  item: Item;
  sizes?: number | string;
  alt: string;
};

export function ResponsiveImage(props: ResponsiveImageProps) {
  const [local, imageProps] = splitProps(props, [
    "item",
    "sizes",
    "alt",
    "loading",
    "decoding",
  ]);
  const source = createMemo(() =>
    responsiveMediaSource(local.item, local.sizes),
  );
  let image: HTMLImageElement | undefined;

  const decodeLoadedImage = () => {
    // Safari can report a loaded image while only painting part of it. Explicit
    // decoding restores the full image, including after a srcset change.
    if (image?.complete && image.naturalWidth > 0) {
      void decodeImageWithin(image, 4_000);
    }
  };

  onMount(() => {
    image?.addEventListener("load", decodeLoadedImage);
    // Cached images may have finished loading before the listener was attached.
    decodeLoadedImage();
  });

  onCleanup(() => {
    image?.removeEventListener("load", decodeLoadedImage);
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
      loading={local.loading ?? "lazy"}
      decoding={local.decoding ?? "async"}
    />
  );
}
