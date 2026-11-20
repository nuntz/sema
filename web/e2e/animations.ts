import { expect, type Locator } from "@playwright/test";

/** Waits until nothing on `locator` is still animating, so a measurement reads
 * where the layout settles rather than an animation's tail. A proxy such as a
 * gap that rounds to its final value can pass while a width is still shrinking
 * (#62). It polls, so an animation that starts after the call still counts;
 * call it once the change that starts the animation is visible. Infinite and
 * paused animations never finish and are skipped. `subtree` includes
 * descendants; `page.locator("html")` with `subtree` covers the document.
 * `bun run lint` keeps `getAnimations` here (no-raw-get-animations.grit). */
export async function animationsFinished(
  locator: Locator,
  { subtree = false } = {},
) {
  await expect
    .poll(() =>
      locator.evaluate(
        (element, subtree) =>
          // biome-ignore lint/plugin: this is the one place specs read animations.
          element
            .getAnimations({ subtree })
            .filter(
              (animation) =>
                (animation.playState === "running" || animation.pending) &&
                animation.effect?.getTiming().iterations !== Infinity,
            ).length,
        subtree,
      ),
    )
    .toBe(0);
}
