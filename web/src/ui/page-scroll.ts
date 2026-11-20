// One nominal frame: how far into the animation its first frame lands.
const FRAME_MS = 1000 / 60;

// A gentle start and finish, with zero velocity and acceleration at both ends.
// Timed on performance.now() alone, never the requestAnimationFrame timestamp:
// that is the frame's start, which can precede the keydown, and under
// SEMA_E2E_STRESS=animation it trails real time by seconds, so mixing the two
// clocks drove t negative and scrolled to the top instead (#63). The clock
// starts one frame before the first frame runs, so that frame always takes a
// first small step even when a loaded main thread runs it late; otherwise the
// next frame could finish the page with no motion between.
export function animatePageScroll(
  element: HTMLElement,
  top: number,
): () => void {
  const from = element.scrollTop;
  const distance = top - from;
  const duration = Math.min(300, Math.max(180, Math.abs(distance) * 0.3));
  let started: number | undefined;
  let frame = 0;
  const tick = () => {
    const now = performance.now();
    started ??= now - FRAME_MS;
    const t = Math.min(1, (now - started) / duration);
    const eased = t * t * t * (t * (t * 6 - 15) + 10);
    element.scrollTo({ top: from + distance * eased, behavior: "instant" });
    if (t < 1) frame = requestAnimationFrame(tick);
  };
  frame = requestAnimationFrame(tick);
  return () => cancelAnimationFrame(frame);
}
