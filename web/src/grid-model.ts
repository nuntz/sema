import { feedbackEligibility } from "./feedback";
import type { ItemWindow } from "./item-view";
import {
  endMarkActionEnabled,
  gridReadStateContext,
  readVisualState,
} from "./layout/read-state";
import type { ScopeCellModel } from "./scope-cell";
import type { FrontPageEntry, GridScope, Item, Order, Story } from "./types";
export interface GridData {
  scopeCell?: ScopeCellModel;
  scope?: GridScope;
  scopeTitle?: string;
  itemWindow?: ItemWindow;
  clearedCount?: number;
  items: Item[];
  entries: FrontPageEntry[];
  stories?: Story[];
  expandedStoryIDs?: ReadonlySet<string>;
  hasMore: boolean;
  archive: boolean;
  unreadOnly: boolean;
  order: Order;
}
export function createGridModel(data: GridData) {
  const readContext = gridReadStateContext(data.archive, data.unreadOnly);
  const cell = (item: Item) => ({
    item,
    archive: data.archive || item.archived === true,
    ...feedbackEligibility(item, data.archive),
    ...readVisualState(readContext, item.read),
  });
  const members = new Map(
    [
      ...data.items,
      ...(data.stories ?? []).flatMap((story) => story.items),
    ].map((item) => [item.item_id, item]),
  );
  const unreadCount = [...members.values()].filter((item) => !item.read).length;
  const canFinish =
    endMarkActionEnabled(readContext) && data.entries.length > 0;
  return { ...data, readContext, cell, unreadCount, canFinish };
}
export type GridModel = ReturnType<typeof createGridModel>;
export interface GridActions {
  onClearScope(): void;
  onShowAll(): void;
  onShowRead(): void;
  onOpenArchive(): void;
  onSelectView(view: "today" | "yesterday"): void;
  onFocus(id: string): void;
  onOpen(item: Item): void;
  onOpenStoryLead(story: Story): void;
  onExternalOpen(item: Item): void;
  onDiscussion(item: Item): void;
  onSignal(item: Item, value: -1 | 0 | 1): void;
  onHeart(item: Item): void;
  onToggleRead(item: Item): void;
  onToggleStoryRead(story: Story): void;
  onCopy(item: Item): void;
  onOriginal(item: Item): void;
  onRelated(item: Item): void;
  onApplyFeed(item: Item): void;
  onMarkBelow(item: Item): void;
  onMarkStoryBelow(storyID: string): void;
  onExpandStory(storyID: string): void;
  onToggleOrder(): void;
  onUndo(): void;
}
