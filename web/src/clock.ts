import { createSignal, onCleanup } from "solid-js";

const [now, setNow] = createSignal(Date.now());
let users = 0;
let timer: ReturnType<typeof setInterval> | undefined;
function refresh() {
  if (document.visibilityState === "visible") setNow(Date.now());
}
function visibility() {
  clearInterval(timer);
  timer = undefined;
  if (document.visibilityState === "visible") {
    refresh();
    timer = setInterval(refresh, 60_000);
  }
}

// One timer shared by mounted grids and readers; sleeping tabs do no work.
export function useClock() {
  if (typeof document !== "undefined") {
    if (users++ === 0) {
      document.addEventListener("visibilitychange", visibility);
      visibility();
    }
    onCleanup(() => {
      if (--users === 0) {
        clearInterval(timer);
        document.removeEventListener("visibilitychange", visibility);
      }
    });
  }
  return now;
}
export { now as clockNow };
