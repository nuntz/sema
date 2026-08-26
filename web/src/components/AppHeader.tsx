import { type JSX, Show } from "solid-js";
import { AppMark } from "./AppMark";

export function AppHeader(props: {
  view: "grid" | "reader";
  onHome(): void;
  tooltipDisabled?: boolean;
  scrolled?: boolean;
  progress?: number;
  children: JSX.Element;
}) {
  const progressPercent = () =>
    Math.round(Math.min(1, Math.max(0, props.progress ?? 0)) * 100);

  return (
    <header
      class="app-header"
      classList={{ "app-header--reader": props.view === "reader" }}
      data-view={props.view}
      data-scrolled={props.scrolled ? "" : undefined}
    >
      <AppMark
        class="app-header__brand"
        onActivate={props.onHome}
        tooltipDisabled={props.tooltipDisabled}
      />
      {props.children}
      <Show when={props.view === "reader"}>
        <div
          class="read-progress"
          role="progressbar"
          aria-label="Read progress"
          aria-valuenow={progressPercent()}
          aria-valuemin="0"
          aria-valuemax="100"
        >
          <i style={{ width: `${progressPercent()}%` }} />
        </div>
      </Show>
    </header>
  );
}
