export type InputKind = "keyboard" | "mouse" | "touch";

/** Which input the user last reached for, and the motion policy it sets (#88). A
 * surface moves or stays put by the last input anywhere, not only the one
 * that triggered it. Enter on a focused button counts as a key, though it
 * arrives as a click, and a pen counts as touch, since it swipes. Listening
 * in the capture phase on the window, before any Overlay's own capture
 * listener, sees keys that an Overlay stops. */
export function createInputModality(target: EventTarget = window) {
  let last: InputKind | undefined;
  const onKey = () => {
    last = "keyboard";
  };
  const onPointer = (event: Event) => {
    const type = (event as PointerEvent).pointerType;
    last = type === "touch" || type === "pen" ? "touch" : "mouse";
  };
  const onTouch = () => {
    last = "touch";
  };
  // Each removal passes the options object its listener was added with:
  // Node 24's EventTarget keeps a capture listener removed with `true`.
  const capture = { capture: true };
  const touchOptions = { capture: true, passive: true };
  target.addEventListener("keydown", onKey, capture);
  target.addEventListener("pointerdown", onPointer, capture);
  target.addEventListener("touchstart", onTouch, touchOptions);
  return {
    last: () => last,
    /** Core navigation, the reader, slides only for touch: its swipe-back
     * drags the reader with the finger, and the slide is how that gesture
     * lands. A key or a mouse repeats it all day, so any slide reads as
     * delay; with no input yet, it stays put. */
    slidesCoreNavigation: () => last === "touch",
    /** An occasional surface, a Peek, the reader's image lightbox or the
     * Dock, moves for any pointer and never for a key; with no input yet, it
     * moves. */
    movesOccasionalSurface: () => last !== "keyboard",
    dispose() {
      target.removeEventListener("keydown", onKey, capture);
      target.removeEventListener("pointerdown", onPointer, capture);
      target.removeEventListener("touchstart", onTouch, touchOptions);
    },
  };
}
