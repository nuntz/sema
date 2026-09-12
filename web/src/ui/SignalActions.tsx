import { For, Show } from "solid-js";
import { Icon } from "../components/Icon";
import {
  buryDisabled,
  clusterActions,
  type SignalValue,
  signalActionLabel,
} from "../signal-feedback";
import type { Item } from "../types";

export function SignalActions(props: {
  item: Item;
  size: "S" | "M" | "L";
  archive?: boolean;
  story?: boolean;
  onSignal(item: Item, value: SignalValue): void;
  onHeart(item: Item): void;
  onMore(): void;
}) {
  const actions = () =>
    props.archive ? (["keep", "more"] as const) : clusterActions(props.size);
  return (
    <div
      class="cell-actions signal-actions"
      classList={{ "story-actions": props.story }}
    >
      <For each={actions()}>
        {(action) => {
          const pressed = () =>
            action === "boost"
              ? props.item.signal === 1
              : action === "bury"
                ? props.item.signal === -1
                : action === "keep"
                  ? props.item.hearted
                  : false;
          const disabled = () => action === "bury" && buryDisabled(props.item);
          const label = () =>
            action === "boost" || action === "bury"
              ? signalActionLabel(props.item.signal, action, props.item.hearted)
              : action === "keep"
                ? props.item.hearted
                  ? "Remove from archive"
                  : "Keep in archive"
                : "More actions";
          return (
            <button
              type="button"
              class={action === "keep" ? "heart" : action}
              classList={{ selected: pressed() }}
              data-action={action}
              aria-label={label()}
              title={label()}
              aria-pressed={action === "more" ? undefined : pressed()}
              aria-haspopup={action === "more" ? "dialog" : undefined}
              disabled={disabled()}
              onPointerDown={(event) => event.stopPropagation()}
              onClick={(event) => {
                event.stopPropagation();
                if (disabled()) return;
                if (action === "boost")
                  props.onSignal(props.item, pressed() ? 0 : 1);
                else if (action === "bury")
                  props.onSignal(props.item, pressed() ? 0 : -1);
                else if (action === "keep") props.onHeart(props.item);
                else props.onMore();
              }}
            >
              <Show
                when={action === "keep"}
                fallback={<Icon name={action} size={14} />}
              >
                <Icon name="keep" size={14} filled={props.item.hearted} />
              </Show>
            </button>
          );
        }}
      </For>
    </div>
  );
}
