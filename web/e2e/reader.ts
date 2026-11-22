import type { Page } from "./test";

/** The reader once it is open. It stays mounted, and matches `.reader`, through
 * its 220ms exit, so `.reader` being visible passes on a reader on its way out:
 * a check that the reader stayed open can't fail, and one that a click opened
 * it passes on the previous reader (#69). `bun run lint` sends specs here
 * (no-closing-reader-visible.grit). */
export const openReader = (page: Page) => page.locator(".reader:not(.closing)");
