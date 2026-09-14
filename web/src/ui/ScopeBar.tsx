// biome-ignore-all lint/a11y/useSemanticElements: The compact control contract uses button-based radio controls.
import { createUniqueId, Show } from "solid-js";
import { Icon } from "../components/Icon";
import { type ItemWindow, scopeSummary } from "../item-view";
import type { ScopeCellModel } from "../scope-cell";
import type { GridScope, Order } from "../types";
import { SourceBadge } from "./SourceBadge";

export function ScopeBar(props: {
  scope: GridScope;
  model?: ScopeCellModel;
  window: ItemWindow;
  unreadOnly: boolean;
  order: Order;
  onClearScope(): void;
  onOrder(order: Order): void;
  onFilter(): void;
}) {
  const lockID = createUniqueId();
  const feedScoped = () => props.scope?.kind === "feed";
  return (
    <div class="scope-bar">
      <Show when={props.scope}>
        <div class="scope-bar-chip">
          <Show when={props.model?.faviconFeed} keyed>
            {(feed) => (
              <SourceBadge
                connector={feed.connector}
                imageURL={feed.favicon_url}
                title={props.model?.title}
                size={20}
              />
            )}
          </Show>
          <span>{props.model?.title}</span>
          <button
            type="button"
            aria-label={feedScoped() ? "Clear feed" : "Clear tag"}
            onClick={props.onClearScope}
          >
            <Icon name="close" size={14} />
          </button>
        </div>
      </Show>
      <div class="scope-order" role="radiogroup" aria-label="Item order">
        <button
          type="button"
          role="radio"
          aria-checked={props.order === "interest"}
          disabled={feedScoped()}
          aria-describedby={feedScoped() ? lockID : undefined}
          onClick={() => props.onOrder("interest")}
        >
          Front page
        </button>
        <button
          type="button"
          role="radio"
          aria-checked={props.order === "chrono"}
          onClick={() => props.onOrder("chrono")}
        >
          Latest
        </button>
      </div>
      <Show when={feedScoped()}>
        <p class="scope-order-lock" id={lockID}>
          <Icon name="lock" size={14} />
          <span>
            Newest first while filtering by feed. Clear the feed to use Front
            page.
          </span>
        </p>
      </Show>
      <button
        type="button"
        class="filter-button"
        aria-haspopup="dialog"
        aria-label={scopeSummary(props.window, props.unreadOnly)}
        onClick={props.onFilter}
      >
        <Icon name="filter" size={16} />
        <span>{scopeSummary(props.window, props.unreadOnly)}</span>
        <span class="scope-count">
          {props.model?.count?.toLocaleString("en-US") ?? "–"}
        </span>
        <Icon name="chevron-down" size={14} />
      </button>
    </div>
  );
}
