import { afterEach, expect, it, vi } from "vitest";
import { animatePageScroll } from "./page-scroll";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const FRAME_MS = 1000 / 60;

// Starts paging 100 → 900 (240ms) at performance.now() = 0. Each tick runs at
// a performance.now() and passes a frame timestamp, which paging must ignore.
function startPaging() {
  let tick: FrameRequestCallback = () => {};
  const now = vi.spyOn(performance, "now").mockReturnValue(0);
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    tick = callback;
    return 1;
  });
  const scrollTo = vi.fn();
  const element = { scrollTop: 100, scrollTo } as unknown as HTMLElement;
  animatePageScroll(element, 900);
  return {
    scrollTo,
    frames: (...times: [real: number, frame: number][]) =>
      times.map(([real, frame]) => {
        now.mockReturnValue(real);
        tick(frame);
        return scrollTo.mock.lastCall?.[0].top as number;
      }),
  };
}

it("eases into and out of paging and finishes at the exact destination", () => {
  const { scrollTo, frames } = startPaging();
  const [early] = frames([FRAME_MS, FRAME_MS]);
  expect(early).toBeGreaterThan(100);
  expect(early).toBeLessThan(105);
  frames([120, 120]);
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 500, behavior: "instant" });
  expect(frames([216, 216])[0]).toBeGreaterThan(880);
  frames([240, 240]);
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 900, behavior: "instant" });
});

// A frame timestamp is the frame's start, which can precede the keydown, and a
// slowed document timeline (SEMA_E2E_STRESS=animation) holds it seconds behind
// performance.now(); mixing the two clocks once scrolled to the top (#63).
it("times paging on performance.now(), not the frame timestamp", () => {
  const { scrollTo, frames } = startPaging();
  const [first, middle] = frames([FRAME_MS, -4000], [120, -3990], [240, -3980]);
  expect(first).toBeGreaterThan(100);
  expect(middle).toBe(500);
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 900, behavior: "instant" });
});

// On a loaded main thread the first frame can run past the whole duration;
// it still takes a first step, so the page doesn't jump in one frame.
it("takes a first small step however late its first frame runs", () => {
  const { frames } = startPaging();
  const [first] = frames([1000, 1000]);
  expect(first).toBeGreaterThan(100);
  expect(first).toBeLessThan(105);
});

it("cancels the pending frame when manual scrolling takes over", () => {
  vi.stubGlobal(
    "requestAnimationFrame",
    vi.fn(() => 42),
  );
  const cancel = vi.fn();
  vi.stubGlobal("cancelAnimationFrame", cancel);
  const stop = animatePageScroll({ scrollTop: 0 } as HTMLElement, 600);
  stop();
  expect(cancel).toHaveBeenCalledWith(42);
});
