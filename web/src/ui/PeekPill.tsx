import { Show } from "solid-js";
import { Icon } from "../components/Icon";
import type { Item } from "../types";

export function PeekPill(props: {
  item: Item;
  size: "S" | "M" | "L";
  onPeek(): void;
}) {
  return (
    <Show
      when={
        props.size !== "S" &&
        props.item.media_url &&
        props.item.media_type !== "video"
      }
    >
      <button
        type="button"
        class="peek-pill"
        aria-label="View images"
        onPointerDown={(event) => event.stopPropagation()}
        onDblClick={(event) => event.stopPropagation()}
        onClick={(event) => {
          event.stopPropagation();
          props.onPeek();
        }}
      >
        <span>
          <Icon name="expand" size={14} />
        </span>
      </button>
    </Show>
  );
}
