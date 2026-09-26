import { createSignal, onCleanup, onMount, Show } from "solid-js";
import { Icon } from "../components/Icon";
import {
  createVideoPlayer,
  type PlaybackState,
  type VideoPlayer as Player,
} from "../youtube-player";

export function VideoPlayer(props: {
  videoID: string;
  start: number;
  onReady?(player: Player): void;
  onState?(state: PlaybackState | "failed"): void;
  onOriginal(): void;
}) {
  let host!: HTMLDivElement;
  const [failed, setFailed] = createSignal(false);
  onMount(() => {
    const player = createVideoPlayer(host, {
      videoID: props.videoID,
      start: props.start,
      onState: (state) => props.onState?.(state),
      onFailure: () => {
        setFailed(true);
        props.onState?.("failed");
      },
    });
    props.onReady?.(player);
    onCleanup(() => player.destroy());
  });
  return (
    <div class="video-embed">
      <div ref={host} class="video-player-host" hidden={failed()} />
      <Show when={failed()}>
        <div class="video-failure" role="status">
          <span>Plays on YouTube only</span>
          <a
            href={`https://www.youtube.com/watch?v=${props.videoID}`}
            target="_blank"
            rel="noopener noreferrer"
            onClick={props.onOriginal}
          >
            Open <Icon name="open-original" />
          </a>
        </div>
      </Show>
    </div>
  );
}
