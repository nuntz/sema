import { createEffect, createSignal, onCleanup, Show } from "solid-js";
import { Icon } from "../components/Icon";
import { type SignalValue, signalLabel, signalWhy } from "../signal-feedback";

export function createSignalFresh(value: () => SignalValue) {
  const [fresh, setFresh] = createSignal(false);
  let previous = value();
  let timer: ReturnType<typeof setTimeout> | undefined;
  createEffect(() => {
    const next = value();
    clearTimeout(timer);
    setFresh(
      next === 1 &&
        previous !== 1 &&
        !window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    );
    previous = next;
    timer = setTimeout(() => setFresh(false), 160);
  });
  onCleanup(() => clearTimeout(timer));
  return fresh;
}
export function SignalLabel(props: { value: SignalValue }) {
  return (
    <Show when={props.value !== 0}>
      <span class="cell-signal-label">{signalLabel(props.value)}</span>
    </Show>
  );
}
export function SignalMarker(props: { value: SignalValue }) {
  return (
    <Show when={props.value !== 0}>
      <span
        class="signal-mobile-chip"
        data-action={props.value === 1 ? "boost" : "bury"}
        aria-hidden="true"
      >
        <Icon name={props.value === 1 ? "boost" : "bury"} size={14} />
      </span>
    </Show>
  );
}
export function SignalWhy(props: {
  value: SignalValue;
  story?: boolean;
  onUndo(): void;
}) {
  return (
    <span class="signal-why">
      {signalWhy(props.value, props.story).replace(" · undo", "")}
      <Show when={props.value === -1}>
        {" · "}
        <button
          type="button"
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            props.onUndo();
          }}
        >
          undo
        </button>
      </Show>
    </span>
  );
}
