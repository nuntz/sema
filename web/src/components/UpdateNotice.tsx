import { createEffect, createSignal, onCleanup, onMount, Show } from "solid-js";
import { type UpdateState, updateMeta } from "../update-notice";
import { Icon } from "./Icon";

export function UpdateNotice(props: {
  state: UpdateState;
  onReload: () => void;
  onDismiss: () => void;
  announcedBuilds: Set<string>;
}) {
  const [now, setNow] = createSignal(Date.now());
  const [message, setMessage] = createSignal("");
  createEffect(() => {
    const build = props.state.build;
    if (props.state.reloading) {
      setMessage("Reloading");
      return;
    }
    if (props.announcedBuilds.has(build)) {
      setMessage("New version ready");
      return;
    }
    setMessage("");
    const announce = window.setTimeout(() => {
      props.announcedBuilds.add(build);
      setMessage("New version ready");
    }, 0);
    onCleanup(() => window.clearTimeout(announce));
  });
  onMount(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000);
    onCleanup(() => {
      window.clearInterval(timer);
    });
  });
  return (
    <div class="update-notice" role="status" aria-live="polite">
      <span class="update-dot" aria-hidden="true" />
      <span>{message()}</span>
      <span class="update-meta" aria-live="off">
        {updateMeta(props.state, now())}
      </span>
      <div class="update-actions">
        <button
          type="button"
          class="update-reload"
          disabled={props.state.reloading}
          aria-disabled={props.state.reloading}
          onClick={props.onReload}
        >
          {props.state.reloading ? "Reloading" : "Reload"}
          <Show when={props.state.reloading}>
            <span class="update-dots" aria-hidden="true">
              <i />
              <i />
              <i />
            </span>
          </Show>
        </button>
        <Show when={!props.state.reloading}>
          <button
            type="button"
            class="update-dismiss"
            aria-label="Dismiss update notice"
            onClick={props.onDismiss}
          >
            <Icon name="close" size={14} />
          </button>
        </Show>
      </div>
    </div>
  );
}
