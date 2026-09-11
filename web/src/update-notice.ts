export type UpdateState = {
  available: boolean;
  build: string;
  builtAt: string;
  dismissed: boolean;
  resurfaced: boolean;
  reloading: boolean;
};

type Version = { build: string; builtAt: string };
type Dependencies = {
  currentBuild: string;
  fetch: (
    url: string,
    init: RequestInit,
  ) => Promise<Pick<Response, "status" | "json">>;
  storage: Pick<Storage, "getItem" | "setItem">;
  now: () => number;
  visible: () => boolean;
  setInterval: (callback: () => void, ms: number) => unknown;
  clearInterval: (timer: unknown) => void;
  changed: (state: UpdateState) => void;
  flush: () => Promise<unknown>;
  reload: () => void;
};

export function createUpdateNotice(deps: Dependencies) {
  const key = "sema:update-dismissed";
  let dismissed = new Set<string>();
  try {
    const saved: unknown = JSON.parse(deps.storage.getItem(key) || "[]");
    if (Array.isArray(saved))
      dismissed = new Set(
        saved.filter((id): id is string => typeof id === "string"),
      );
  } catch {
    /* Storage can be unavailable. */
  }
  let state: UpdateState = {
    available: false,
    build: "",
    builtAt: "",
    dismissed: false,
    resurfaced: false,
    reloading: false,
  };
  let candidate: Version | undefined;
  let lastCheck = -Infinity;
  let pending: Promise<void> | undefined;
  let disposed = false;
  const publish = () => {
    if (
      disposed ||
      !candidate ||
      state.reloading ||
      candidate.build === state.build
    )
      return;
    const hidden = dismissed.has(candidate.build);
    state = {
      ...state,
      ...candidate,
      available: !hidden,
      dismissed: hidden,
      resurfaced: dismissed.size > 0 && !hidden,
    };
    deps.changed(state);
  };
  const check = async (onReturn = false) => {
    if (disposed || !deps.visible()) return;
    if (!pending && deps.now() - lastCheck >= 60_000) {
      lastCheck = deps.now();
      pending = (async () => {
        try {
          const response = await deps.fetch("/version.json", {
            cache: "no-store",
          });
          if (response.status !== 200) return;
          const version: unknown = await response.json();
          if (
            !version ||
            typeof version !== "object" ||
            !("build" in version) ||
            !("builtAt" in version) ||
            typeof version.build !== "string" ||
            !version.build ||
            typeof version.builtAt !== "string" ||
            !Number.isFinite(Date.parse(version.builtAt))
          )
            return;
          candidate =
            version.build === deps.currentBuild
              ? undefined
              : { build: version.build, builtAt: version.builtAt };
        } catch {
          /* A failed check never changes the notice. */
        }
      })();
      await pending;
      pending = undefined;
    } else if (pending) await pending;
    // Interval discoveries wait for a window return before changing layout.
    if (onReturn && deps.visible()) publish();
  };
  const timer = deps.setInterval(() => void check(), 15 * 60_000);
  return {
    get state() {
      return state;
    },
    onReturn: () => check(true),
    dismiss() {
      if (!state.available || state.reloading) return;
      dismissed.add(state.build);
      try {
        deps.storage.setItem(key, JSON.stringify([...dismissed]));
      } catch {
        /* Keep the session dismissal. */
      }
      state = { ...state, available: false, dismissed: true };
      deps.changed(state);
    },
    async reload() {
      if (!state.available || state.reloading) return;
      state = { ...state, reloading: true };
      deps.changed(state);
      try {
        await deps.flush();
      } finally {
        deps.reload();
      }
    },
    dispose() {
      disposed = true;
      deps.clearInterval(timer);
    },
  };
}

export function updateMeta(state: UpdateState, now: number): string {
  const date = new Date(state.builtAt);
  const time = `${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
  if (state.resurfaced) return `second deploy · ${time}`;
  const minutes = Math.max(0, Math.floor((now - date.getTime()) / 60_000));
  return `${minutes >= 60 ? `deployed at ${time}` : `deployed ${minutes} min ago`} · ${state.build}`;
}
