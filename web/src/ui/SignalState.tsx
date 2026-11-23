import { createEffect, createSignal, onCleanup, Show } from "solid-js";
import { Icon } from "../components/Icon";
import { type SignalValue, signalWhy } from "../signal-feedback";

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
/** A phone cell's Signal chip. A live Kept Item is always Boosted, so a Kept
 * cell's chip is the Kept one and no Kept marker stacks under it (#84). */
export function SignalMarker(props: { value: SignalValue; kept?: boolean }) {
  const action = () =>
    props.value === 1 ? (props.kept ? "keep" : "boost") : "bury";
  return (
    <Show when={props.value !== 0}>
      <span
        class="signal-mobile-chip"
        data-action={action()}
        aria-hidden="true"
      >
        <Icon
          name={action()}
          size={14}
          filled={action() === "keep" ? true : undefined}
        />
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
      <span class="signal-why-text">
        {signalWhy(props.value, props.story).replace(" · undo", "")}
      </span>
      <Show when={props.value === -1}>
        <span class="signal-why-undo">
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
        </span>
      </Show>
    </span>
  );
}
