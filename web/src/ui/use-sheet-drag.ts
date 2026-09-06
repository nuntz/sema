import { createMemo, createSignal, onCleanup } from "solid-js";
import {
  beginSheetDrag,
  type SheetDragGesture,
  sheetDragOffset,
  sheetScrimOpacity,
  shouldDismissSheet,
} from "./sheet-drag";

const SHEET_TRANSITION_MS = 180;

export function useSheetDrag(options: {
  panel(): HTMLElement | undefined;
  onDismiss(): void;
  enabled?(): boolean;
  scrollTop?(target: EventTarget | null): number | undefined;
}) {
  const [offset, setOffset] = createSignal(0);
  const [dragging, setDragging] = createSignal(false);
  let gesture: SheetDragGesture | undefined;
  let pointerID: number | undefined;
  let deferDragForScroll = false;
  let dismissTimer: number | undefined;

  const startDragging = (event: PointerEvent) => {
    setDragging(true);
    (event.currentTarget as HTMLElement).setPointerCapture?.(event.pointerId);
  };

  const releasePointer = (event: PointerEvent) => {
    const target = event.currentTarget as HTMLElement;
    if (target.hasPointerCapture?.(event.pointerId))
      target.releasePointerCapture(event.pointerId);
  };

  const clearGesture = (event: PointerEvent) => {
    releasePointer(event);
    gesture = undefined;
    pointerID = undefined;
    deferDragForScroll = false;
    setDragging(false);
  };

  const onPointerDown = (event: PointerEvent) => {
    if (options.enabled && !options.enabled()) return;
    const scrollTop = options.scrollTop?.(event.target);
    const next = beginSheetDrag(
      event.pointerType,
      event.clientY,
      performance.now(),
      scrollTop ?? 0,
    );
    if (!next) return;
    gesture = next;
    pointerID = event.pointerId;
    deferDragForScroll = scrollTop !== undefined;
    if (!deferDragForScroll) startDragging(event);
  };

  const onPointerMove = (event: PointerEvent) => {
    if (!gesture || event.pointerId !== pointerID) return;
    if (!dragging()) {
      const distance = event.clientY - gesture.startY;
      if (Math.abs(distance) < 4) return;
      if (deferDragForScroll && distance < 0) {
        clearGesture(event);
        return;
      }
      startDragging(event);
    }
    if (event.cancelable) event.preventDefault();
    setOffset(sheetDragOffset(gesture, event.clientY));
  };

  const onPointerUp = (event: PointerEvent) => {
    if (!gesture || event.pointerId !== pointerID) return;
    if (!dragging()) {
      clearGesture(event);
      return;
    }
    const panel = options.panel();
    const height = panel?.clientHeight ?? 1;
    const dismiss = shouldDismissSheet(
      gesture,
      event.clientY,
      performance.now(),
      height,
    );
    const moved = Math.abs(offset()) > 4;
    clearGesture(event);
    if (moved && event.cancelable) event.preventDefault();
    if (!dismiss) {
      setOffset(0);
      return;
    }
    const rect = panel?.getBoundingClientRect();
    setOffset(
      rect ? offset() + window.innerHeight - rect.top : window.innerHeight,
    );
    const duration = window.matchMedia("(prefers-reduced-motion: reduce)")
      .matches
      ? 0
      : SHEET_TRANSITION_MS;
    dismissTimer = window.setTimeout(() => {
      options.onDismiss();
      setOffset(0);
    }, duration);
  };

  const onPointerCancel = (event: PointerEvent) => {
    if (!gesture || event.pointerId !== pointerID) return;
    clearGesture(event);
    setOffset(0);
  };

  const scrimOpacity = createMemo(() =>
    sheetScrimOpacity(offset(), options.panel()?.clientHeight ?? 1),
  );

  onCleanup(() => window.clearTimeout(dismissTimer));

  return {
    offset,
    dragging,
    scrimOpacity,
    onPointerDown,
    onPointerMove,
    onPointerUp,
    onPointerCancel,
  };
}
