/**
 * Where a phone ends for the viewport: a viewport narrower than this gets the
 * phone headers, reader, lightbox and Dock. styles.css states it as
 * `(max-width: 619px)` and `(min-width: 620px)`, which styles-tokens.test.ts
 * holds to this value. The grid's phone edge is separate (`phoneGridMaxWidth`).
 */
export const phoneViewportMaxWidth = 620;
