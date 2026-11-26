import {
  createEffect,
  createSignal,
  on,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { Portal } from "solid-js/web";
import { Icon } from "../components/Icon";
import type { Item } from "../types";
import type { StageState, VideoDock } from "../video-dock";
import { videoItemID } from "../video-item";
import type { VideoPlayer as VideoPlayerHandle } from "../youtube-player";
import { VideoPeek } from "./VideoPeek";
import { VideoPlayer } from "./VideoPlayer";

const FLIP_MS = 260;
const FLIP_EASING = "cubic-bezier(.32, .72, 0, 1)";

/**
 * The single host for a Peeked or Docked video. It never moves in the DOM;
 * changing mode restyles it in place so the iframe keeps playing.
 */
export function VideoStage(props: {
  dock: VideoDock;
  /** Another Overlay covers the Dock, which then waits dimmed and inert. */
  covered: boolean;
  /** Whether a Peek opening, Docking or Expanding now may move (input-modality.ts). */
  mayAnimate(): boolean;
  onExpand(): void;
  onFlip(item: Item): void;
  onOriginal(item: Item): void;
}) {
  return (
    <Show when={props.dock.current()}>
      {(video) => (
        <Stage
          dock={props.dock}
          item={video().item}
          videoKey={video().key}
          start={video().start}
          covered={props.covered}
          mayAnimate={props.mayAnimate}
          onExpand={props.onExpand}
          onFlip={() => props.onFlip(video().item)}
          onOriginal={() => props.onOriginal(video().item)}
        />
      )}
    </Show>
  );
}

function Stage(props: {
  dock: VideoDock;
  item: Item;
  videoKey: number;
  start: number;
  covered: boolean;
  mayAnimate(): boolean;
  onExpand(): void;
  onFlip(): void;
  onOriginal(): void;
}) {
  let layer!: HTMLDivElement;
  let stage!: HTMLDivElement;
  // The Peek on top, if any, lends its close to the scrim.
  let closePeek: (() => void) | undefined;
  let player: VideoPlayerHandle | undefined;
  const [state, setState] = createSignal<StageState>("loading");
  const mode = () => props.dock.mode();
  const docked = () => mode() === "dock";

  const syncTabbable = () => {
    // Docked, YouTube's controls would trap Tab; the strip stays reachable.
    const frame = stage.querySelector("iframe");
    if (frame) frame.tabIndex = docked() ? -1 : 0;
  };

  createEffect(
    on(
      () => props.videoKey,
      () => setState("loading"),
      { defer: true },
    ),
  );
  createEffect(() => {
    stage.inert = docked() && props.covered;
  });
  // Docked, the stage is a landmark; Peeked, the dialog speaks for it.
  createEffect(() => {
    if (docked()) {
      stage.setAttribute("role", "region");
      stage.setAttribute("aria-label", `Docked video: ${props.item.title}`);
    } else {
      stage.removeAttribute("role");
      stage.removeAttribute("aria-label");
    }
  });
  createEffect(
    on(mode, (next) => {
      const first = stage.dataset.mode
        ? stage.getBoundingClientRect()
        : undefined;
      // Moving or in place, the scrim follows the stage.
      const mayAnimate = props.mayAnimate();
      layer.toggleAttribute("data-instant", !mayAnimate);
      // The Dock's `bottom` transition rides the reader's toolbar; the switch
      // itself lands at once, then moves only by the FLIP below.
      stage.style.transition = "none";
      stage.dataset.mode = next;
      stage.getBoundingClientRect();
      stage.style.transition = "";
      syncTabbable();
      if (
        !mayAnimate ||
        !first?.width ||
        window.matchMedia("(prefers-reduced-motion: reduce)").matches
      )
        return;
      const last = stage.getBoundingClientRect();
      if (!last.width || !last.height) return;
      stage.animate(
        [
          {
            transformOrigin: "top left",
            transform: `translate(${first.left - last.left}px, ${first.top - last.top}px) scale(${first.width / last.width}, ${first.height / last.height})`,
          },
          { transformOrigin: "top left", transform: "none" },
        ],
        { duration: FLIP_MS, easing: FLIP_EASING },
      );
    }),
  );

  onMount(() => {
    const visibility = () =>
      props.dock.setVisible(document.visibilityState !== "hidden");
    // YouTube's controls keep every key once used; leaving the Dock hands
    // the keyboard back to the grid or reader beside it.
    const release = () => {
      const active = document.activeElement;
      if (
        docked() &&
        !document.fullscreenElement &&
        active instanceof HTMLIFrameElement &&
        stage.contains(active)
      )
        active.blur();
    };
    visibility();
    const ticker = window.setInterval(() => props.dock.tick(), 1_000);
    document.addEventListener("visibilitychange", visibility);
    stage.addEventListener("pointerleave", release);
    onCleanup(() => {
      window.clearInterval(ticker);
      document.removeEventListener("visibilitychange", visibility);
      stage.removeEventListener("pointerleave", release);
    });
  });

  return (
    <Portal>
      {/* Peeked, this layer is the dialog. It contains the stage, so the
          player sits inside the modal and Shift+Tab out of it lands here. */}
      <div ref={layer} class="video-layer">
        {/* The scrim outlives its Peek, so it can fade out under the stage
            as the stage moves to the Dock. */}
        <div
          class="video-scrim"
          aria-hidden="true"
          onClick={() => closePeek?.()}
        />
        <Show when={!docked()}>
          <VideoPeek
            item={props.item}
            layer={() => layer}
            provideClose={(close) => {
              closePeek = close;
            }}
            stage={() => stage}
            state={state}
            onDismiss={() => props.dock.dismissPeek()}
            onFlip={props.onFlip}
            onToggle={() => player?.toggle()}
          />
        </Show>
        <div
          ref={stage}
          class="video-stage"
          classList={{ "is-covered": docked() && props.covered }}
        >
          {/* Grid and reader treat Enter as a command; these buttons keep it. */}
          <div
            class="video-dock-strip"
            hidden={!docked()}
            on:keydown={(event) => {
              if (event.key === "Enter" || event.key === " ")
                event.stopPropagation();
            }}
          >
            <span class="video-dock-title">{props.item.title}</span>
            <button
              type="button"
              class="video-dock-action"
              aria-label="Expand video"
              onClick={props.onExpand}
            >
              <Icon name="expand" size={15} />
            </button>
            <button
              type="button"
              class="video-dock-action"
              aria-label="Close video"
              onClick={() => props.dock.close()}
            >
              <Icon name="close" size={15} />
            </button>
          </div>
          <Show when={props.videoKey} keyed>
            {(_key) => (
              <VideoPlayer
                videoID={videoItemID(props.item) || ""}
                start={props.start}
                onReady={(ready) => {
                  player = ready;
                  props.dock.attach(() => ready.position());
                  syncTabbable();
                }}
                onState={(next) => {
                  setState(next);
                  props.dock.setState(next);
                }}
                onOriginal={props.onOriginal}
              />
            )}
          </Show>
        </div>
      </div>
    </Portal>
  );
}
