// A text-only cell's summary gets the whole lines between its title and its
// bottom line, the element that follows it. The summary must not size the
// space it fills (it shrinks in a flex column, or sits in a grid row), so the
// bottom line sits at the cell's foot and the space never depends on the clamp.
export function fitSummaryLines(summary: HTMLElement): () => void {
  const clamp = () => {
    const bottomLine = summary.nextElementSibling;
    if (!bottomLine) return;
    const style = getComputedStyle(summary);
    const lineHeight = Number.parseFloat(style.lineHeight);
    if (!lineHeight) return;
    const available =
      bottomLine.getBoundingClientRect().top -
      summary.getBoundingClientRect().top -
      (Number.parseFloat(style.marginBottom) || 0);
    const lines = Math.floor((available + 0.5) / lineHeight);
    summary.style.setProperty("--summary-lines", String(Math.max(1, lines)));
    summary.toggleAttribute("data-no-room", lines < 1);
  };
  const observer = new ResizeObserver(() => {
    const copy = summary.parentElement;
    if (!copy) return;
    // Watch what bounds the space, not the summary, so clamping can't loop.
    observer.unobserve(summary);
    observer.observe(copy);
    for (const child of copy.children)
      if (child !== summary) observer.observe(child);
    clamp();
  });
  observer.observe(summary);
  return () => observer.disconnect();
}
