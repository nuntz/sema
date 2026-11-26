import { expect, type Locator, type Page } from "@playwright/test";

/** Waits until nothing on `locator` is still animating, so a measurement reads
 * where the layout settles rather than an animation's tail. A proxy such as a
 * gap that rounds to its final value can pass while a width is still shrinking
 * (#62). It polls, so an animation that starts after the call still counts;
 * call it once the change that starts the animation is visible. Infinite and
 * paused animations never finish and are skipped. `subtree` includes
 * descendants; `page.locator("html")` with `subtree` covers the document.
 * `bun run lint` keeps `getAnimations` in this file (no-raw-get-animations.grit). */
export async function animationsFinished(
  locator: Locator,
  { subtree = false } = {},
) {
  await expect
    .poll(() =>
      locator.evaluate(
        (element, subtree) =>
          // biome-ignore lint/plugin: this and startedAnimations are the only places specs read animations.
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

/** Every animation on `locator` and its descendants, each as "what on which
 * element": a CSS animation's name, `transition:<property>`, or `animate()`
 * for one started from script, then the target's classes. A test proving a
 * surface opens in place polls this for the full list with the timeline held
 * (`animationTimeline`), rather than the properties its author thought of: a
 * held animation stays listed, whatever element and property it moves (#89).
 * `except` names selectors whose motion isn't the surface's, such as an image
 * that fades in once it loads. Infinite animations, such as a spinner, are
 * skipped, as in `animationsFinished`. Poll for the list the step settles on:
 * a timer the page runs, such as the Lightbox's idle dimming after 3s, adds
 * its own transitions to a poll that runs that long. */
export function startedAnimations(
  locator: Locator,
  { except = [] as string[] } = {},
) {
  return locator.evaluate((element, except) => {
    const describe = (animation: Animation) => {
      if (animation instanceof CSSAnimation) return animation.animationName;
      if (animation instanceof CSSTransition)
        return `transition:${animation.transitionProperty}`;
      return "animate()";
    };
    return (
      // biome-ignore lint/plugin: this and animationsFinished are the only places specs read animations.
      element
        .getAnimations({ subtree: true })
        .filter(
          (animation) => animation.effect?.getTiming().iterations !== Infinity,
        )
        .flatMap((animation) => {
          const target = (animation.effect as KeyframeEffect | null)?.target;
          if (!target || except.some((selector) => target.matches(selector)))
            return [];
          return [
            `${describe(animation)} on .${[...target.classList].join(".")}`,
          ];
        })
        .sort()
    );
  }, except);
}

/** Freezes the page's animation timeline (`hold(true)`) or lets it run again,
 * so a check reads the first frame of whatever a step starts, and a value an
 * animation would still be moving is final: a slide that ran would sit at its
 * start for as long as the check polls. Releasing restores the playback rate
 * SEMA_E2E_STRESS=animation sets (./test.ts). Chromium only. */
export async function animationTimeline(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Animation.enable");
  const running = process.env.SEMA_E2E_STRESS === "animation" ? 0.1 : 1;
  return (held: boolean) =>
    cdp.send("Animation.setPlaybackRate", {
      playbackRate: held ? 0 : running,
    });
}
