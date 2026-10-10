import { createEffect, on, onCleanup, onMount } from "solid-js";
import type { Item } from "../types";
import { closeOverlay, keyOwnership, pushOverlay } from "./overlay-history";

/**
 * The Peek around a stage-mounted video. It lends dialog semantics to the
 * stage's layer, which also holds the player, so dismissing the Peek can Dock
 * the player without moving, and so reloading, its iframe.
 */
export function VideoPeek(props: {
  item: Item;
  layer(): HTMLElement;
  stage(): HTMLElement;
  state(): string;
  onDismiss(): void;
  onFlip(): void;
}) {
  let dialog!: HTMLElement;
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
      props.stage().contains(active)
    )
      dialog.focus({ preventScroll: true });
  };
  const close = (flip = false) => {
    if (closing) return;
    closing = true;
    closeOverlay("lightbox");
    if (flip) props.onFlip();
    else props.onDismiss();
  };
  createEffect(
    on(
      props.state,
      (state) => {
        if (state === "playing" || state === "paused" || state === "ended")
          returnFocus();
      },
      { defer: true },
    ),
  );
  createEffect(() => {
    dialog?.setAttribute("aria-label", `Play ${props.item.title}`);
  });
  onMount(() => {
    dialog = props.layer();
    const stage = props.stage();
    const previous = document.activeElement;
    dialog.classList.add("lb-overlay", "video-peek");
    dialog.setAttribute("role", "dialog");
    dialog.setAttribute("aria-modal", "true");
    dialog.setAttribute("aria-label", `Play ${props.item.title}`);
    dialog.tabIndex = 0;
    const siblings = Array.from(document.body.children).filter(
      (el): el is HTMLElement =>
        el instanceof HTMLElement &&
        !el.contains(dialog) &&
        !el.contains(stage),
    );
    const inert = siblings.map((el) => el.inert);
    siblings.forEach((el) => {
      el.inert = true;
    });
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    pushOverlay("lightbox", () => close());
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
        close(event.key === "o");
      }
      if (event.key === "Tab") {
        const controls = [
          dialog,
          ...stage.querySelectorAll<HTMLElement>(".video-embed :is(iframe, a)"),
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
    stage.addEventListener("pointerleave", returnFocus);
    onCleanup(() => {
      closing = true;
      window.removeEventListener("keydown", key, true);
      document.removeEventListener("fullscreenchange", returnFocus);
      stage.removeEventListener("pointerleave", returnFocus);
      closeOverlay("lightbox");
      dialog.classList.remove("lb-overlay", "video-peek");
      for (const name of ["role", "aria-modal", "aria-label", "tabindex"])
        dialog.removeAttribute(name);
      siblings.forEach((el, i) => {
        el.inert = inert[i];
      });
      document.body.style.overflow = overflow;
      if (previous instanceof HTMLElement && previous.isConnected)
        previous.focus({ preventScroll: true });
    });
  });
  return <div class="lb-scrim" onClick={() => close()} aria-hidden="true" />;
}
