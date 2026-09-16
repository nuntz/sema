import { afterEach, expect, it, vi } from "vitest";
import { animatePageScroll } from "./page-scroll";

afterEach(() => vi.restoreAllMocks());

it("eases into and out of paging and finishes at the exact destination", () => {
  let tick: FrameRequestCallback = () => {};
  vi.spyOn(performance, "now").mockReturnValue(0);
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    tick = callback;
    return 1;
  });
  const scrollTo = vi.fn();
  const element = { scrollTop: 100, scrollTo } as unknown as HTMLElement;
  animatePageScroll(element, 900);
  tick(24);
  const early = scrollTo.mock.lastCall?.[0].top;
  expect(early).toBeGreaterThan(100);
  expect(early).toBeLessThan(120);
  tick(120);
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 500, behavior: "instant" });
  tick(216);
  expect(scrollTo.mock.lastCall?.[0].top).toBeGreaterThan(880);
  tick(240);
  expect(scrollTo).toHaveBeenLastCalledWith({ top: 900, behavior: "instant" });
  vi.unstubAllGlobals();
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
  vi.unstubAllGlobals();
});
