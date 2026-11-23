import { expect, type Locator } from "./test";

// Waits until a scroller's extent (scrollHeight - clientHeight) has held for
// half a second and returns it, for a test that asserts a scroll lands exactly
// on its bottom. Heights measured after the test's last wait can still move
// the extent: in the grid, ResizeObserver measurements such as the top slot's
// or a Story Lead's, which resizeGrid's layout markers don't cover, and
// headline heights, which carry no marker at all; in the reader, body content
// that lays out late. #83 read the grid's extent once and missed the bottom
// moving up 2px. It settles what is rendered now: grid rows that mount only
// after a scroll can move it again. On its cap the poll fails with every
// reading it took.
export async function settledScrollExtent(scroller: Locator) {
  const holdMs = 500;
  const readings: { at: number; extent: number }[] = [];
  await expect
    .poll(
      async () => {
        const extent = await scroller.evaluate(
          (element: HTMLElement) => element.scrollHeight - element.clientHeight,
        );
        const at = Date.now();
        readings.push({ at, extent });
        let runStart = readings.length - 1;
        while (runStart > 0 && readings[runStart - 1].extent === extent)
          runStart--;
        const held = at - readings[runStart].at;
        if (held >= holdMs) return "held";
        const start = readings[0].at;
        return `held ${held}ms of ${holdMs}ms; readings: ${readings
          .map((reading) => `${reading.extent} at ${reading.at - start}ms`)
          .join(", ")}`;
      },
      { intervals: [100] },
    )
    .toBe("held");
  return readings[readings.length - 1].extent;
}
