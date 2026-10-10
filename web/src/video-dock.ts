import { createSignal } from "solid-js";
import type { Item } from "./types";
import type { PlaybackState } from "./youtube-player";

/** Matches the server's dwell threshold for a meaningful view. */
export const DWELL_THRESHOLD_MS = 30_000;

export type DockOrigin = "peek" | "reader";
export type StageMode = "peek" | "dock";
export type StageState = PlaybackState | "loading" | "failed";

/** One Video Item on the stage. A new key means a fresh player must load. */
export interface StageVideo {
  key: number;
  item: Item;
  origin: DockOrigin;
  start: number;
  archive: boolean;
}

export interface Handoff {
  seconds: number;
  dwellMS: number;
}

const live = (state: StageState) =>
  state === "playing" || state === "buffering";

export const expandTarget = (
  video: Pick<StageVideo, "origin">,
  readerOpen: boolean,
): StageMode | "reader" =>
  video.origin === "peek" && !readerOpen ? "peek" : "reader";

/**
 * The one video that outlives its Peek or reader. The stage keeps a single
 * player for both modes, because moving a cross-origin iframe reloads it.
 */
export function createVideoDock(now = () => performance.now()) {
  const [current, setCurrent] = createSignal<StageVideo>();
  const [mode, setMode] = createSignal<StageMode>("peek");
  let nextKey = 0;
  let state: StageState = "loading";
  let visible = true;
  let position = () => 0;
  let report: (itemID: string, dwellMS: number) => void = () => {};
  let dwellMS = 0;
  let since: number | undefined;
  let lastReported = 0;
  let thresholdReported = false;

  const dwell = () => dwellMS + (since === undefined ? 0 : now() - since);
  const flush = () => {
    const video = current();
    const total = Math.round(dwell());
    if (!video || video.archive || total <= lastReported) return;
    lastReported = total;
    report(video.item.item_id, total);
  };
  const sync = () => {
    const accruing = !!current() && mode() === "dock" && live(state) && visible;
    if (accruing) {
      since ??= now();
      return;
    }
    if (since === undefined) return;
    dwellMS += now() - since;
    since = undefined;
    flush();
  };
  const begin = (
    item: Item,
    origin: DockOrigin,
    nextMode: StageMode,
    start: number,
    carried: number,
    archive: boolean,
  ) => {
    end();
    state = "loading";
    dwellMS = carried;
    lastReported = carried;
    thresholdReported = carried >= DWELL_THRESHOLD_MS;
    position = () => start;
    setMode(nextMode);
    setCurrent({ key: ++nextKey, item, origin, start, archive });
  };
  const end = () => {
    sync();
    flush();
    since = undefined;
    position = () => 0;
    setCurrent(undefined);
  };

  return {
    current,
    mode,
    /** Peek a Video Item. Peeking the Docked Item Expands it instead of a new Play. */
    peek(item: Item): boolean {
      if (current()?.item.item_id === item.item_id) {
        setMode("peek");
        sync();
        return false;
      }
      begin(item, "peek", "peek", 0, 0, item.archived === true);
      return true;
    },
    /** A dismissed Peek Docks a live video and ends anything else. */
    dismissPeek() {
      if (!current() || mode() !== "peek") return;
      if (!live(state)) {
        end();
        return;
      }
      setMode("dock");
      sync();
    },
    /** A reader leaving a live lead video hands it to a fresh Docked player. */
    dockFromReader(
      item: Item,
      seconds: number,
      carried: number,
      archive: boolean,
    ) {
      begin(item, "reader", "dock", Math.max(0, seconds), carried, archive);
    },
    /** Return the Docked Item to its reader, which resumes from here. */
    take(itemID: string): Handoff | undefined {
      if (current()?.item.item_id !== itemID) return;
      sync();
      const handoff = {
        seconds: position(),
        dwellMS: Math.round(dwell()),
      };
      end();
      return handoff;
    },
    expandToPeek() {
      if (!current()) return;
      setMode("peek");
      sync();
    },
    close() {
      if (current()) end();
    },
    /** Stage hooks: the mounted player and the page it plays in. */
    attach(read: () => number) {
      position = read;
    },
    setState(next: StageState) {
      if (!current()) return;
      state = next;
      sync();
    },
    setVisible(next: boolean) {
      visible = next;
      sync();
    },
    setReporter(next: (itemID: string, dwellMS: number) => void) {
      report = next;
    },
    tick() {
      if (!current() || thresholdReported || dwell() < DWELL_THRESHOLD_MS)
        return;
      thresholdReported = true;
      flush();
    },
  };
}

export type VideoDock = ReturnType<typeof createVideoDock>;
