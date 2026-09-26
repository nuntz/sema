import { createSignal, Show } from "solid-js";
import { render } from "solid-js/web";
import { Icon } from "../components/Icon";
import { youtubeVideoID } from "../video-item";
import type { PlaybackState } from "../youtube-player";
import { VideoPlayer } from "./VideoPlayer";

/** Upgrade extracted YouTube cards without treating body links as Video Items. */
export function mountReaderVideos(
  body: HTMLElement,
  callbacks: {
    onPlay(): void;
    onState(card: Element, state: PlaybackState | "loading" | "failed"): void;
  },
): () => void {
  const cleanups: Array<() => void> = [];
  for (const link of body.querySelectorAll<HTMLAnchorElement>(
    "a.media-card[href]",
  )) {
    const videoID = youtubeVideoID(link.href);
    if (!videoID) continue;
    const host = document.createElement("div");
    host.className = "reader-video";
    const poster = link.querySelector("img")?.cloneNode(true);
    const url = link.href;
    link.replaceWith(host);
    const dispose = render(() => {
      const [playing, setPlaying] = createSignal(false);
      return (
        <div class="video-media-card">
          <Show
            when={playing()}
            fallback={
              <button
                type="button"
                class="video-media-band"
                aria-label={`Play ${link.title || "YouTube video"}`}
                onClick={() => {
                  callbacks.onPlay();
                  callbacks.onState(host, "loading");
                  setPlaying(true);
                }}
              >
                {poster}
                <span class="video-card-play" aria-hidden="true">
                  <Icon name="play" size={20} filled />
                </span>
              </button>
            }
          >
            <VideoPlayer
              videoID={videoID}
              start={0}
              onOriginal={callbacks.onPlay}
              onState={(state) => callbacks.onState(host, state)}
            />
          </Show>
          <a
            class="video-provider-strip"
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            onClick={callbacks.onPlay}
          >
            <b>YOUTUBE</b>
            <i />
            <span>{url.replace(/^https?:\/\//, "")}</span>
            <strong>
              Open
              <Icon name="open-original" />
            </strong>
          </a>
        </div>
      );
    }, host);
    cleanups.push(() => {
      dispose();
      host.replaceWith(link);
    });
  }
  return () => {
    for (const cleanup of cleanups) cleanup();
  };
}
