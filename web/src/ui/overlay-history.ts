import { createSignal } from "solid-js";

export type OverlayKind =
  | "reader"
  | "lightbox"
  | "action-sheet"
  | "related"
  | "feeds"
  | "keyboard-help"
  | "confirm-remove"
  | "search"
  | "filter-sheet"
  | "feeds-dialog";

export type KeyOwner = OverlayKind | "grid" | "transient";
export interface KeyOwnership {
  owner: KeyOwner;
  overlays: readonly OverlayKind[];
}

interface HistoryState {
  sema?: unknown;
  semaOverlayID?: unknown;
  semaOverlaySession?: unknown;
  [key: string]: unknown;
}

export interface OverlayHistoryLike {
  readonly state: unknown;
  pushState(data: unknown, unused: string): void;
  back(): void;
}

export interface PopStateSource {
  addEventListener(
    type: "popstate",
    listener: (event: PopStateEvent) => void,
  ): void;
  removeEventListener(
    type: "popstate",
    listener: (event: PopStateEvent) => void,
  ): void;
}

interface OverlayEntry {
  id: number;
  kind: OverlayKind;
  onPop: () => void;
  pushed: boolean;
  history: boolean;
}

export interface OverlayHistory {
  pushOverlay(kind: OverlayKind, onPop: () => void, history?: boolean): void;
  keyboard(transient?: boolean, base?: "grid" | "reader"): KeyOwnership;
  closeOverlay(kind: OverlayKind): void;
  destroy(): void;
}

let sessionSequence = 0;

export function createOverlayHistory(
  history: OverlayHistoryLike,
  events: PopStateSource,
): OverlayHistory {
  const session = `${Date.now()}-${++sessionSequence}`;
  let nextID = 0;
  let programmaticPops = 0;
  const entries: OverlayEntry[] = [];
  const [stack, setStack] = createSignal<OverlayKind[]>([]);
  const publish = () => setStack(entries.map((entry) => entry.kind));

  const stateFor = (entry: OverlayEntry): HistoryState => {
    const current = history.state;
    const state =
      current && typeof current === "object"
        ? { ...(current as HistoryState) }
        : {};
    return {
      ...state,
      sema: entry.kind,
      semaOverlayID: entry.id,
      semaOverlaySession: session,
    };
  };

  const pushEntry = (entry: OverlayEntry) => {
    history.pushState(stateFor(entry), "");
    entry.pushed = true;
  };

  const flushDeferredPushes = () => {
    if (programmaticPops > 0) return;
    for (const entry of entries) {
      if (entry.history && !entry.pushed) pushEntry(entry);
    }
  };

  const onPopState = (event: PopStateEvent) => {
    if (programmaticPops > 0) {
      programmaticPops--;
      flushDeferredPushes();
      return;
    }

    const state = event.state as HistoryState | null;
    const targetIndex =
      state?.semaOverlaySession === session
        ? entries.findIndex(
            (entry) => entry.pushed && entry.id === state.semaOverlayID,
          )
        : -1;
    const popped = entries.splice(targetIndex + 1);
    publish();
    for (let index = popped.length - 1; index >= 0; index--)
      popped[index].onPop();
  };

  events.addEventListener("popstate", onPopState);

  return {
    keyboard(transient = false, base = "grid") {
      // Reader remains mounted during its exit animation after its history pop.
      const overlays =
        base === "reader" && !stack().includes("reader")
          ? ["reader" as const, ...stack()]
          : stack();
      return {
        owner: transient ? "transient" : (overlays.at(-1) ?? "grid"),
        overlays,
      };
    },

    pushOverlay(kind, onPop, history = true) {
      const existing = entries.find((entry) => entry.kind === kind);
      if (existing) {
        existing.onPop = onPop;
        return;
      }
      const entry: OverlayEntry = {
        id: ++nextID,
        kind,
        onPop,
        pushed: false,
        history,
      };
      entries.push(entry);
      publish();
      if (history && programmaticPops === 0) pushEntry(entry);
    },

    closeOverlay(kind) {
      const index = entries.findIndex((entry) => entry.kind === kind);
      if (index < 0) return;
      const [entry] = entries.splice(index, 1);
      publish();
      if (!entry.pushed) return;
      programmaticPops++;
      history.back();
    },

    destroy() {
      events.removeEventListener("popstate", onPopState);
      entries.length = 0;
      publish();
    },
  };
}

let browserOverlayHistory: OverlayHistory | undefined;

function browserHistory(): OverlayHistory {
  if (!browserOverlayHistory)
    browserOverlayHistory = createOverlayHistory(window.history, window);
  return browserOverlayHistory;
}

export function pushOverlay(
  kind: OverlayKind,
  onPop: () => void,
  history = true,
): void {
  browserHistory().pushOverlay(kind, onPop, history);
}

export function closeOverlay(kind: OverlayKind): void {
  browserHistory().closeOverlay(kind);
}

export function keyOwnership(
  transient = false,
  base: "grid" | "reader" = "grid",
): KeyOwnership {
  return browserHistory().keyboard(transient, base);
}
