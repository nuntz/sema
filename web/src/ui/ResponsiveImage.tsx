import {
  createMemo,
  createSignal,
  type JSX,
  onCleanup,
  onMount,
  splitProps,
  useContext,
} from "solid-js";
import { decodeImageWithin } from "../image-decode";
import { responsiveMediaSource } from "../media-image";
import type { Item } from "../types";
import {
  GridPixelRatioContext,
  gridImageOverscan,
  ImageLoadingEnabledContext,
} from "./image-loading";

type ResponsiveImageProps = Omit<
  JSX.ImgHTMLAttributes<HTMLImageElement>,
  "src" | "srcset" | "sizes" | "alt"
> & {
  item: Item;
  sizes?: number | string;
  alt: string;
  deferUntilVisible?: boolean;
  maxDimension?: number;
};

export function ResponsiveImage(props: ResponsiveImageProps) {
  const imagesEnabled = useContext(ImageLoadingEnabledContext);
  const [local, imageProps] = splitProps(props, [
    "item",
    "sizes",
    "alt",
    "loading",
    "decoding",
    "deferUntilVisible",
    "maxDimension",
  ]);
  const [enabled, setEnabled] = createSignal(!local.deferUntilVisible);
  // Grid widths are known. Select a plain src to avoid retaining detached
  // cards through the browser's native srcset viewport-change listeners.
  const pixelRatio = useContext(GridPixelRatioContext);
  const source = createMemo(() =>
    responsiveMediaSource(
      local.item,
      local.sizes,
      local.maxDimension,
      local.deferUntilVisible && typeof local.sizes === "number"
        ? pixelRatio()
        : undefined,
    ),
  );
  let image: HTMLImageElement | undefined;
  let observer: IntersectionObserver | undefined;
  const decodeLoadedImage = () => {
    // Keep the reader's Safari paint workaround. Grid images use native
    // decoding so repeated virtual-cell mounts don't force full-size decodes.
    if (!local.deferUntilVisible && image?.complete && image.naturalWidth > 0) {
      void decodeImageWithin(image, 4_000);
    }
  };

  onMount(() => {
    if (!imagesEnabled) return;
    if (!local.deferUntilVisible) {
      image?.addEventListener("load", decodeLoadedImage);
      // Cached images may have loaded before the listener was attached.
      decodeLoadedImage();
    }
    if (local.deferUntilVisible && image) {
      if (typeof IntersectionObserver === "undefined") {
        setEnabled(true);
      } else {
        // Root the margin at the scroller: the document viewport's margin
        // would still be clipped by the grid's overflow container.
        const root = image.closest(".grid-scroll");
        const margin = gridImageOverscan(
          root?.clientHeight ?? window.innerHeight,
        );
        observer = new IntersectionObserver(
          ([entry]) => {
            // Edge contact counts as intersecting at the default threshold.
            // Rejecting its zero ratio can strand the image: moving farther
            // inside does not cross another threshold or trigger a callback.
            if (entry.isIntersecting) {
              observer?.disconnect();
              setEnabled(true);
            }
          },
          { root, rootMargin: `${margin}px 0px` },
        );
        observer.observe(image);
      }
    }
  });

  onCleanup(() => {
    observer?.disconnect();
    image?.removeEventListener("load", decodeLoadedImage);
    // Drop references to photo resources on disposal. This is separate from
    // the detached-card fix, which avoids native srcset in the grid above.
    image?.removeAttribute("srcset");
    image?.removeAttribute("src");
  });

  return (
    <img
      {...imageProps}
      ref={image}
      src={imagesEnabled && enabled() ? source().src : undefined}
      alt={local.alt}
      srcset={imagesEnabled && enabled() ? source().srcset : undefined}
      sizes={source().sizes}
      loading={local.loading ?? "lazy"}
      decoding={local.decoding ?? "async"}
    />
  );
}
