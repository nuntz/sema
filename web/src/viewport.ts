/**
 * The viewport widths the layout switches at. Each `...MaxWidth` is the first
 * width that no longer gets the narrow layout: a viewport narrower than it
 * does. styles.css states each as a max-width one pixel under it and a
 * min-width at it, TypeScript builds its media queries from these, and e2e
 * specs take their widths from them; styles-tokens.test.ts and
 * no-literal-width-edge.grit hold all three to these values.
 */

/**
 * Where a phone ends for the viewport: a viewport narrower than this gets the
 * phone headers, reader, lightbox, Dock, sheets and type scale. It equals the
 * grid's phone edge (`phoneGridMaxWidth`), which is measured on the grid
 * rather than the viewport (#86).
 */
export const phoneViewportMaxWidth = 724;

/** Below this, the grid header is 44px and drops its leading tooltip. */
export const smallPhoneMaxWidth = 431;

/**
 * Below this, the grid header trades its display controls and segments for
 * the Scope chip, filter summary and tag strip, and the reader compacts.
 */
export const compactHeaderMaxWidth = 860;

/** At this and wider, the reader's identity aligns to the article measure. */
export const wideReaderMinWidth = 1200;

/** The media query a viewport narrower than `width` matches. */
export const narrowerThan = (width: number) => `(max-width: ${width - 1}px)`;

/** The media query a phone viewport matches. */
export const phoneViewportQuery = narrowerThan(phoneViewportMaxWidth);
