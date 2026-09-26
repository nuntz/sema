import { onCleanup, onMount } from "solid-js";
import { Portal } from "solid-js/web";
import type { Item } from "../types";
import { videoItemID } from "../video-item";
import type { VideoPlayer as Player } from "../youtube-player";
import { closeOverlay, keyOwnership, pushOverlay } from "./overlay-history";
import { VideoPlayer } from "./VideoPlayer";

export function VideoPeek(props: {
  item: Item;
  onClose(): void;
  onFlip(seconds: number): void;
  onOriginal(): void;
}) {
  let dialog!: HTMLDivElement;
  let player: Player | undefined;
  let closing = false;
  const returnFocus = () => {
    // YouTube's cross-origin controls cannot forward Escape. Once playback
    // settles (or the pointer leaves), return its keyboard to the Peek.
    const active = document.activeElement;
    if (
      !closing &&
      keyOwnership().owner === "lightbox" &&
      document.visibilityState !== "hidden" &&
      !document.fullscreenElement &&
      active instanceof HTMLIFrameElement &&
      dialog.contains(active)
    )
      dialog.focus({ preventScroll: true });
  };
  const close = () => {
    if (closing) return;
    closing = true;
    player?.destroy();
    closeOverlay("lightbox");
    props.onClose();
  };
  onMount(() => {
    const previous = document.activeElement;
    const siblings = Array.from(document.body.children).filter(
      (el): el is HTMLElement =>
        el instanceof HTMLElement && !el.contains(dialog),
    );
    const inert = siblings.map((el) => el.inert);
    siblings.forEach((el) => {
      el.inert = true;
    });
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    pushOverlay("lightbox", close);
    dialog.focus();
    // Cross-origin controls own their keys; Shift+Tab can return to this dialog.
    const key = (event: KeyboardEvent) => {
      if (
        keyOwnership().owner !== "lightbox" ||
        event.defaultPrevented ||
        event.isComposing ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey
      )
        return;
      if (["Escape", "i", "o"].includes(event.key)) {
        event.preventDefault();
        event.stopImmediatePropagation();
        const seconds = player?.position() ?? 0;
        close();
        if (event.key === "o") props.onFlip(seconds);
      }
      if (event.key === "Tab") {
        const controls = [
          dialog,
          ...dialog.querySelectorAll<HTMLElement>("iframe, a"),
        ];
        const index = controls.indexOf(document.activeElement as HTMLElement);
        event.preventDefault();
        controls[
          (index + (event.shiftKey ? -1 : 1) + controls.length) %
            controls.length
        ]?.focus();
      }
    };
    window.addEventListener("keydown", key, true);
    document.addEventListener("fullscreenchange", returnFocus);
    onCleanup(() => {
      window.removeEventListener("keydown", key, true);
      document.removeEventListener("fullscreenchange", returnFocus);
      closeOverlay("lightbox");
      siblings.forEach((el, i) => {
        el.inert = inert[i];
      });
      document.body.style.overflow = overflow;
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus({ preventScroll: true });
    });
  });
  return (
    <Portal>
      <div
        ref={dialog}
        class="lb-overlay video-peek"
        role="dialog"
        aria-modal="true"
        aria-label={`Play ${props.item.title}`}
        tabindex="0"
      >
        <div class="lb-scrim" onClick={close} aria-hidden="true" />
        <div class="video-peek-frame" onPointerLeave={returnFocus}>
          <VideoPlayer
            videoID={videoItemID(props.item) || ""}
            start={0}
            onReady={(value) => {
              player = value;
            }}
            onState={(state) => {
              if (
                state === "playing" ||
                state === "paused" ||
                state === "ended"
              )
                returnFocus();
            }}
            onOriginal={props.onOriginal}
          />
        </div>
      </div>
    </Portal>
  );
}
