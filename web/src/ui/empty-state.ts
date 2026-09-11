import type { ItemView } from "../item-view";
import type { GridScope, Order } from "../types";

export interface EmptyStateAction {
  label: string;
  key?: string;
  run(): void;
}

export interface EmptyStateInputs {
  scope?: GridScope;
  scopeTitle?: string;
  itemView?: ItemView;
  order: Order;
  phone: boolean;
  clearedCount?: number;
  onClearScope?(): void;
  onShowAll?(): void;
  onShowRead?(): void;
  onToggleOrder?(): void;
  onOpenArchive?(): void;
  onSelectView?(view: "today" | "yesterday"): void;
}

export function emptyState(input: EmptyStateInputs) {
  const count = input.clearedCount ?? 0;
  const body =
    count > 0
      ? `You cleared ${count} ${count === 1 ? "item" : "items"}. New ones land here as they are fetched.`
      : "Nothing unread here right now. New arrivals land here as they are fetched.";
  const showRead: EmptyStateAction = {
    label: "Show read items",
    key: "A",
    run: () => input.onShowRead?.(),
  };
  let heading = "You're all caught up";
  let actions: EmptyStateAction[];
  // Scoped and day views hang from the scope cell, so they read as the tail
  // of a section. The unscoped caught-up state has nothing above it and is
  // centered in the viewport instead.
  let centered = false;
  if (input.scope) {
    const tag = input.scope.kind === "tag";
    heading = `End of ${input.scopeTitle ?? `${tag ? "#" : ""}${input.scope.value}`}`;
    const clear: EmptyStateAction = {
      label: tag ? "Clear tag" : "Clear feed",
      key: "Esc",
      run: () => input.onClearScope?.(),
    };
    actions = input.phone ? [clear, showRead] : [showRead, clear];
    if (tag)
      actions.push({
        label: input.order === "chrono" ? "Go to Front page" : "Go to Latest",
        key: "T",
        run: () => input.onToggleOrder?.(),
      });
  } else if (input.itemView === "today" || input.itemView === "yesterday") {
    const today = input.itemView === "today";
    heading = today ? "Nothing new today" : "Nothing from yesterday";
    actions = [
      { label: "Show all", key: "G A", run: () => input.onShowAll?.() },
      {
        label: today ? "Yesterday" : "Today",
        key: today ? "G Y" : "G T",
        run: () => input.onSelectView?.(today ? "yesterday" : "today"),
      },
    ];
  } else {
    centered = true;
    actions = [
      showRead,
      { label: "Archive", key: "G R", run: () => input.onOpenArchive?.() },
    ];
  }
  return { heading, body, actions, centered };
}
