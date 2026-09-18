import { type Accessor, createContext } from "solid-js";

// Scoped to the grid for the ?grid-images=off memory experiment. Keep image
// elements and layout metadata, but omit their sources, including favicons.
export const ImageLoadingEnabledContext = createContext(true);

// Owned by the grid so all mounted images share one resize listener.
export const GridPixelRatioContext = createContext<Accessor<number>>(() => 1);

// Mount and load the next page in place, while keeping the buffer above small.
export function gridImageLookahead(viewportHeight: number): {
  top: number;
  bottom: number;
} {
  const buffer = Math.min(240, viewportHeight / 4);
  return { top: buffer, bottom: viewportHeight + buffer };
}
