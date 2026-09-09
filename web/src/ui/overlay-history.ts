export type OverlayKind =
  | "reader"
  | "lightbox"
  | "action-sheet"
  | "related"
  | "feeds"
  | "keyboard-help"
  | "confirm-remove"
  | "search";

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
}

export interface OverlayHistory {
  pushOverlay(kind: OverlayKind, onPop: () => void): void;
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
      if (!entry.pushed) pushEntry(entry);
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
    for (let index = popped.length - 1; index >= 0; index--)
      popped[index].onPop();
  };

  events.addEventListener("popstate", onPopState);

  return {
    pushOverlay(kind, onPop) {
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
      };
      entries.push(entry);
      if (programmaticPops === 0) pushEntry(entry);
    },

    closeOverlay(kind) {
      const index = entries.findIndex((entry) => entry.kind === kind);
      if (index < 0) return;
      const [entry] = entries.splice(index, 1);
      if (!entry.pushed) return;
      programmaticPops++;
      history.back();
    },

    destroy() {
      events.removeEventListener("popstate", onPopState);
      entries.length = 0;
    },
  };
}

let browserOverlayHistory: OverlayHistory | undefined;

function browserHistory(): OverlayHistory {
  if (!browserOverlayHistory)
    browserOverlayHistory = createOverlayHistory(window.history, window);
  return browserOverlayHistory;
}

export function pushOverlay(kind: OverlayKind, onPop: () => void): void {
  browserHistory().pushOverlay(kind, onPop);
}

export function closeOverlay(kind: OverlayKind): void {
  browserHistory().closeOverlay(kind);
}
