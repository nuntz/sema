/** The only boundary to YouTube: callers own the host and destroy on unmount. */
export interface VideoPlayer {
  play(seconds?: number): void;
  position(): number;
  destroy(): void;
}
export type PlaybackState = "playing" | "paused" | "ended" | "buffering";
export interface PlayerOptions {
  videoID: string;
  start: number;
  onState(state: PlaybackState): void;
  onFailure(): void;
}
const EMBED_ORIGIN = "https://www.youtube-nocookie.com";
let nextPlayerID = 0;

export function createVideoPlayer(
  host: HTMLElement,
  options: PlayerOptions,
): VideoPlayer {
  const iframe = document.createElement("iframe");
  const id = `sema-video-${++nextPlayerID}`;
  let disposed = false;
  let failed = false;
  let ready = false;
  let listening = false;
  let position = Math.max(0, options.start);
  let pendingSeek: number | undefined = position > 0 ? position : undefined;
  const send = (message: object) =>
    iframe.contentWindow?.postMessage(
      JSON.stringify({ ...message, id, channel: "widget" }),
      EMBED_ORIGIN,
    );
  const command = (func: string, args: unknown[] = []) =>
    send({ event: "command", func, args });
  const cleanup = () => {
    clearTimeout(timeout);
    window.removeEventListener("offline", fail);
    window.removeEventListener("message", receive);
    iframe.removeEventListener("load", loaded);
    iframe.remove();
  };
  const fail = () => {
    if (disposed || failed) return;
    failed = true;
    cleanup();
    options.onFailure();
  };
  const loaded = () => {
    if (disposed || failed) return;
    listening = true;
    send({ event: "listening" });
  };
  const receive = (event: MessageEvent) => {
    if (
      disposed ||
      failed ||
      !listening ||
      event.origin !== EMBED_ORIGIN ||
      event.source !== iframe.contentWindow ||
      typeof event.data !== "string"
    )
      return;
    let message: {
      event?: unknown;
      info?: { currentTime?: unknown; playerState?: unknown };
    } | null;
    try {
      message = JSON.parse(event.data);
    } catch {
      return;
    }
    if (!message || typeof message !== "object") return;
    if (message.event === "onError") {
      fail();
      return;
    }
    if (message.event !== "onReady" && message.event !== "infoDelivery") return;
    if (message.event === "infoDelivery") {
      const info = message.info;
      if (
        typeof info?.currentTime === "number" &&
        Number.isFinite(info.currentTime) &&
        info.currentTime >= 0
      ) {
        position = info.currentTime;
        iframe.dataset.seconds = String(position);
      }
      const state = (
        {
          0: "ended",
          1: "playing",
          2: "paused",
          3: "buffering",
          5: "paused",
        } as const
      )[info?.playerState as 0 | 1 | 2 | 3 | 5];
      if (typeof info?.playerState === "number" && state) {
        iframe.dataset.state = String(info.playerState);
        options.onState(state);
      }
    }
    if (!ready) {
      ready = true;
      clearTimeout(timeout);
      if (pendingSeek !== undefined) command("seekTo", [pendingSeek, true]);
      pendingSeek = undefined;
      command("playVideo");
    }
  };
  const timeout = window.setTimeout(fail, 15_000);
  iframe.title = "YouTube video player";
  iframe.allow = "autoplay; fullscreen; picture-in-picture; encrypted-media";
  iframe.referrerPolicy = "strict-origin-when-cross-origin";
  iframe.allowFullscreen = true;
  iframe.dataset.videoId = options.videoID;
  iframe.dataset.host = EMBED_ORIGIN;
  iframe.dataset.seconds = String(position);
  iframe.src = `${EMBED_ORIGIN}/embed/${encodeURIComponent(options.videoID)}?enablejsapi=1&autoplay=1&playsinline=1&start=${Math.floor(position)}&origin=${encodeURIComponent(location.origin)}`;
  iframe.addEventListener("load", loaded);
  window.addEventListener("message", receive);
  window.addEventListener("offline", fail);
  if (!navigator.onLine) queueMicrotask(fail);
  else host.append(iframe);
  return {
    play(seconds) {
      if (disposed || failed) return;
      if (seconds !== undefined && Number.isFinite(seconds))
        pendingSeek = Math.max(0, seconds);
      if (ready) {
        if (pendingSeek !== undefined) command("seekTo", [pendingSeek, true]);
        pendingSeek = undefined;
        command("playVideo");
      }
    },
    position: () => position,
    destroy() {
      if (disposed) return;
      disposed = true;
      if (!failed) cleanup();
    },
  };
}

export function videoDwellActive(
  state: PlaybackState | "poster" | "loading" | "failed",
  visible: boolean,
  focused: boolean,
): boolean {
  return (
    visible &&
    (state === "playing" ||
      state === "buffering" ||
      ((state === "poster" || state === "failed") && focused))
  );
}
