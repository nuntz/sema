import { type JSX, Show } from "solid-js";

export function Tooltip(props: {
  name: string;
  shortcut?: string;
  disabled?: boolean;
  align?: "start" | "center" | "end";
  children: JSX.Element;
}) {
  return (
    <span
      class="tooltip-wrap"
      classList={{
        "tooltip-start": props.align === "start",
        "tooltip-end": props.align === "end",
      }}
    >
      {props.children}
      <Show when={!props.disabled}>
        <span class="tooltip-bubble" role="tooltip">
          <span>{props.name}</span>
          <Show when={props.shortcut}>
            {(shortcut) => <kbd>{shortcut()}</kbd>}
          </Show>
        </span>
      </Show>
    </span>
  );
}
