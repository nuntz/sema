import { createEffect, createSignal, onCleanup, Show } from "solid-js";
import type { SignalNotice } from "../signal-feedback";

export function SignalHint(props: {
  notice?: SignalNotice;
  visible: boolean;
  onUndo(notice: SignalNotice): void;
}) {
  const [expired, setExpired] = createSignal(false);
  createEffect(() => {
    const notice = props.notice;
    setExpired(false);
    if (!notice) return;
    const timer = window.setTimeout(() => setExpired(true), 6000);
    onCleanup(() => window.clearTimeout(timer));
  });
  return (
    <Show when={props.visible && props.notice}>
      <div
        class="signal-hint"
        classList={{ "signal-hint--expired": expired() }}
        role="status"
        aria-live="polite"
      >
        <i aria-hidden="true" />
        <span>{props.notice?.text}</span>
        <Show when={props.notice?.undo}>
          <button
            type="button"
            onClick={() => {
              const notice = props.notice;
              if (notice) props.onUndo(notice);
            }}
          >
            Undo
          </button>
        </Show>
      </div>
    </Show>
  );
}
