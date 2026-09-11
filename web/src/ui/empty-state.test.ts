import { expect, it, vi } from "vitest";
import { type EmptyStateInputs, emptyState } from "./empty-state";

const base: EmptyStateInputs = { order: "interest", phone: false };
const states: [Partial<EmptyStateInputs>, string, string[], string][] = [
  [
    { scope: { kind: "tag", value: "ai" } },
    "End of #ai",
    ["Show read items", "Clear tag", "Go to Latest"],
    "Clear tag",
  ],
  [
    { scope: { kind: "feed", value: "verge" }, scopeTitle: "The Verge" },
    "End of The Verge",
    ["Show read items", "Clear feed"],
    "Clear feed",
  ],
  [
    { itemView: "today" },
    "Nothing new today",
    ["Show all", "Yesterday"],
    "Show all",
  ],
  [
    { itemView: "yesterday" },
    "Nothing from yesterday",
    ["Show all", "Today"],
    "Show all",
  ],
  [
    { itemView: "unread" },
    "You're all caught up",
    ["Show read items", "Archive"],
    "Show read items",
  ],
];
for (const [input, heading, labels, primary] of states) {
  it(heading, () => {
    const result = emptyState({ ...base, ...input });
    expect(result.heading).toBe(heading);
    expect(result.actions.map((action) => action.label)).toEqual(labels);
    expect(
      emptyState({ ...base, ...input, phone: true }).actions[0].label,
    ).toBe(primary);
    expect(result.body).toBe(
      "Nothing unread here right now. New arrivals land here as they are fetched.",
    );
    for (const count of [0, 1, 14])
      expect(emptyState({ ...base, ...input, clearedCount: count }).body).toBe(
        count === 0
          ? result.body
          : `You cleared ${count} ${count === 1 ? "item" : "items"}. New ones land here as they are fetched.`,
      );
  });
}
it("uses the current order for tag navigation", () => {
  expect(
    emptyState({
      ...base,
      scope: { kind: "tag", value: "ai" },
      order: "chrono",
    }).actions[2],
  ).toMatchObject({ label: "Go to Front page", key: "T" });
});
it("dispatches actions to their matching callbacks and exposes existing shortcuts", () => {
  const callbacks = {
    onShowRead: vi.fn(),
    onClearScope: vi.fn(),
    onToggleOrder: vi.fn(),
    onShowAll: vi.fn(),
    onSelectView: vi.fn(),
    onOpenArchive: vi.fn(),
  };
  for (const [input] of states)
    for (const action of emptyState({ ...base, ...input, ...callbacks })
      .actions)
      action.run();
  expect(callbacks.onShowRead).toHaveBeenCalledTimes(3);
  expect(callbacks.onClearScope).toHaveBeenCalledTimes(2);
  expect(callbacks.onToggleOrder).toHaveBeenCalledTimes(1);
  expect(callbacks.onShowAll).toHaveBeenCalledTimes(2);
  expect(callbacks.onSelectView.mock.calls).toEqual([["yesterday"], ["today"]]);
  expect(callbacks.onOpenArchive).toHaveBeenCalledTimes(1);
  expect(emptyState(base).actions.map((action) => action.key)).toEqual([
    "A",
    "G R",
  ]);
});
