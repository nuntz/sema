import type { JSX } from "solid-js";
import { Icon } from "./Icon";
import { Tooltip } from "./Tooltip";

const LABEL = "Sema — back to top";

export function AppMark(props: {
  onActivate(): void;
  tooltipDisabled?: boolean;
  class?: string;
}) {
  const activate: JSX.EventHandlerUnion<HTMLAnchorElement, MouseEvent> = (
    event,
  ) => {
    if (
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    )
      return;
    event.preventDefault();
    props.onActivate();
  };

  return (
    <Tooltip name={LABEL} disabled={props.tooltipDisabled} align="start">
      <a
        href="/"
        class={`app-mark ${props.class ?? ""}`.trim()}
        aria-label={LABEL}
        onClick={activate}
      >
        <Icon name="newspaper" size={20} />
        <span>Sema</span>
      </a>
    </Tooltip>
  );
}
