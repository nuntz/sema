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
interface YouTubePlayer {
  playVideo(): void;
  seekTo(seconds: number, allowSeekAhead: boolean): void;
  getCurrentTime(): number;
  destroy(): void;
}
interface YouTubeAPI {
  Player: new (
    host: HTMLElement,
    options: {
      host: string;
      videoId: string;
      playerVars: Record<string, string | number>;
      events: {
        onReady(event: { target: YouTubePlayer }): void;
        onStateChange(event: { data: number }): void;
        onError(): void;
      };
    },
  ) => YouTubePlayer;
}
declare global {
  interface Window {
    YT?: YouTubeAPI;
    onYouTubeIframeAPIReady?: () => void;
  }
}
let apiPromise: Promise<YouTubeAPI> | undefined;
function loadAPI(): Promise<YouTubeAPI> {
  if (window.YT?.Player) return Promise.resolve(window.YT);
  if (apiPromise) return apiPromise;
  apiPromise = new Promise<YouTubeAPI>((resolve, reject) => {
    const script = document.createElement("script");
    const previous = window.onYouTubeIframeAPIReady;
    const finish = (error?: Error) => {
      clearTimeout(timeout);
      window.onYouTubeIframeAPIReady = previous;
      script.onerror = null;
      if (error) {
        script.remove();
        reject(error);
      } else if (window.YT?.Player) resolve(window.YT);
      else reject(new Error("YouTube API unavailable"));
    };
    const timeout = window.setTimeout(
      () => finish(new Error("YouTube API timed out")),
      15_000,
    );
    window.onYouTubeIframeAPIReady = () => {
      finish();
      previous?.();
    };
    script.onerror = () => finish(new Error("YouTube API unavailable"));
    script.src = "https://www.youtube.com/iframe_api";
    document.head.append(script);
  }).catch((error) => {
    apiPromise = undefined;
    throw error;
  });
  return apiPromise;
}

export function createVideoPlayer(
  host: HTMLElement,
  options: PlayerOptions,
): VideoPlayer {
  let player: YouTubePlayer | undefined;
  let disposed = false;
  let failed = false;
  let ready = false;
  let position = options.start;
  let timeout = 0;
  const fail = () => {
    if (disposed || failed) return;
    failed = true;
    clearTimeout(timeout);
    player?.destroy();
    player = undefined;
    options.onFailure();
  };
  const offline = () => fail();
  window.addEventListener("offline", offline);
  if (!navigator.onLine) queueMicrotask(fail);
  else
    void loadAPI()
      .then((api) => {
        if (disposed || failed) return;
        // The API replaces this child, leaving Solid's host under Solid's ownership.
        const mount = document.createElement("div");
        host.append(mount);
        timeout = window.setTimeout(fail, 15_000);
        player = new api.Player(mount, {
          host: "https://www.youtube-nocookie.com",
          videoId: options.videoID,
          playerVars: {
            autoplay: 1,
            playsinline: 1,
            start: Math.floor(position),
            origin: location.origin,
          },
          events: {
            onReady: ({ target }) => {
              if (disposed || failed) {
                target.destroy();
                return;
              }
              clearTimeout(timeout);
              ready = true;
              target.seekTo(position, true);
              target.playVideo();
            },
            onStateChange: ({ data }) => {
              if (disposed || failed) return;
              const state = (
                {
                  0: "ended",
                  1: "playing",
                  2: "paused",
                  3: "buffering",
                  5: "paused",
                } as const
              )[data as 0 | 1 | 2 | 3 | 5];
              if (state) options.onState(state);
            },
            onError: fail,
          },
        });
      })
      .catch(fail);
  return {
    play(seconds) {
      if (disposed || failed) return;
      if (seconds !== undefined) position = Math.max(0, seconds);
      if (ready) {
        if (seconds !== undefined) player?.seekTo(position, true);
        player?.playVideo();
      }
    },
    position: () =>
      ready && !failed ? (player?.getCurrentTime() ?? position) : position,
    destroy() {
      if (disposed) return;
      disposed = true;
      clearTimeout(timeout);
      window.removeEventListener("offline", offline);
      player?.destroy();
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
