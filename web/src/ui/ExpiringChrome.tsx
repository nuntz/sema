import { Show } from "solid-js";
import { Icon } from "../components/Icon";

export interface ExpiringCounts {
  within48h: number;
  tonight: number;
  unread: number;
}
export interface ExpiringScope {
  label: string;
  kind: "tag" | "feed";
}
export function ExpiringStrip(props: {
  counts: ExpiringCounts;
  scope?: ExpiringScope;
  showing?: number;
}) {
  return (
    <div class="expiring-strip">
      <Icon name="clock" size={16} />
      <span class="expiring-strip-desktop">
        {props.counts.within48h} items
        {props.scope
          ? ` ${props.scope.kind === "tag" ? "in" : "from"} ${props.scope.label}`
          : ""}{" "}
        go in the next two days — {props.counts.tonight} of them tonight.
        <span class="expiring-strip-advice">
          {" "}
          · Keep the ones you want; the rest need nothing from you.
        </span>
      </span>
      <span class="expiring-strip-phone">
        {props.counts.tonight} go tonight · {props.counts.within48h} in two days
        {props.scope ? ` · ${props.scope.label}` : ""}
      </span>
      <Show when={props.showing !== undefined}>
        <span class="expiring-showing">showing the first {props.showing}</span>
      </Show>
      <span class="expiring-sort">sorted soonest first</span>
    </div>
  );
}
export function ExpiringEnd(props: {
  empty: boolean;
  remaining: number;
  top: number;
  onBack(): void;
}) {
  return (
    <section
      class="expiring-end"
      classList={{ "expiring-empty": props.empty }}
      style={{ top: `${props.top}px` }}
    >
      <Show
        when={props.empty}
        fallback={
          <>
            <div class="expiring-closing-rule">
              THAT IS ALL OF THEM
              <i />
            </div>
            <p>
              {props.remaining} more unread items have over two days left ·{" "}
              <button type="button" onClick={props.onBack}>
                back to unread <kbd>g u</kbd>
              </button>
            </p>
          </>
        }
      >
        <Icon name="clock" size={24} />
        <h2>Nothing goes in the next two days</h2>
        <p>
          Items go seven days after they were published; anything you keep with
          ♥ stays for good.
        </p>
        <button type="button" onClick={props.onBack}>
          back to unread <kbd>g u</kbd>
        </button>
      </Show>
    </section>
  );
}
