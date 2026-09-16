// A gentle start and finish, with zero velocity and acceleration at both ends.
export function animatePageScroll(
  element: HTMLElement,
  top: number,
): () => void {
  const from = element.scrollTop;
  const distance = top - from;
  const duration = Math.min(300, Math.max(180, Math.abs(distance) * 0.3));
  const started = performance.now();
  let frame = 0;
  const tick = (now: number) => {
    const t = Math.min(1, (now - started) / duration);
    const eased = t * t * t * (t * (t * 6 - 15) + 10);
    element.scrollTo({ top: from + distance * eased, behavior: "instant" });
    if (t < 1) frame = requestAnimationFrame(tick);
  };
  frame = requestAnimationFrame(tick);
  return () => cancelAnimationFrame(frame);
}
