/**
 * Where a phone ends for the viewport: a viewport narrower than this gets the
 * phone headers, reader, lightbox, Dock, sheets and type scale. styles.css
 * states it as a max-width one pixel under it and a min-width at it, and
 * TypeScript builds its media queries from it; styles-tokens.test.ts holds
 * both to this value. It equals the grid's phone edge (`phoneGridMaxWidth`),
 * which is measured on the grid rather than the viewport (#86).
 */
export const phoneViewportMaxWidth = 724;

/** The media query a phone viewport matches. */
export const phoneViewportQuery = `(max-width: ${phoneViewportMaxWidth - 1}px)`;
