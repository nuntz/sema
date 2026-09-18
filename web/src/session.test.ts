import { afterEach, describe, expect, it, vi } from "vitest";
import { createGridModel } from "./grid-model";
import { createItemSession } from "./item-session";
import { createReadState, type ReadGeometry } from "./read-state";
import type { Item, ItemsResponse, Story } from "./types";

const item = (id: string): Item => ({
  item_id: id,
  feed_id: "feed",
  url: `https://example.com/${id}`,
  title: id,
  summary_source: "",
  published_ts: "2026-09-17T00:00:00Z",
  fetched_ts: "2026-09-17T00:00:00Z",
  has_body: false,
  extract_quality: 0,
  score: 0.5,
  size: "M",
  read: false,
  signal: 0,
  hearted: false,
});
function setup() {
  const api = {
    items: vi.fn(
      async (): Promise<ItemsResponse> => ({ items: [], next_cursor: "" }),
    ),
    stories: vi.fn(async () => ({ stories: [] })),
    archive: vi.fn(async () => ({ items: [], next_cursor: "" })),
    read: vi.fn(async (_id: string, _read: boolean) => {}),
    readBatch: vi.fn(async (_ids: string[], _read: boolean) => {}),
    behaviour: vi.fn(async () => {}),
  };
  const errors = vi.fn();
  const session = createItemSession(api, {
    onError: errors,
    changed: () => {},
    beforeReload: () => {},
    pagingBlocked: () => false,
    cleared: () => undefined,
    visible: () => true,
  });
  const reads = createReadState(api, session, errors);
  return { api, session, reads, errors };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}
afterEach(() => vi.useRealTimers());
describe("Item-list session", () => {
  it("does not page or mark anything for an unloaded mark-below anchor", async () => {
    const { api, session, reads } = setup();
    api.items.mockResolvedValueOnce({
      items: [item("loaded")],
      next_cursor: "later",
    });
    await session.reload();
    api.items.mockClear();
    api.items.mockResolvedValue({ items: [item("later")], next_cursor: "" });
    await reads.markBelow("story:missing");
    await reads.markBelow("missing");
    await reads.flushRead();
    expect(api.items).not.toHaveBeenCalled();
    expect(api.readBatch).not.toHaveBeenCalled();
    expect(session.items()[0].read).toBe(false);
    reads.dispose();
    session.dispose();
  });

  it("ignores a stale page when a later scope load wins", async () => {
    const { api, session } = setup();
    const first = deferred<ItemsResponse>();
    api.items
      .mockImplementationOnce(() => first.promise)
      .mockResolvedValueOnce({ items: [item("new")], next_cursor: "" });
    const old = session.reload("chrono");
    await session.reload("chrono");
    first.resolve({ items: [item("old")], next_cursor: "old-cursor" });
    await old;
    expect(session.items().map((i) => i.item_id)).toEqual(["new"]);
    expect(session.cursor()).toBe("");
    session.dispose();
  });
  it("patches every loaded copy without dropping other fields", () => {
    const { session } = setup();
    const original = item("same");
    session.setItems([original]);
    session.setStories([
      {
        story_id: "story",
        items: [original],
        source_count: 2,
        order_key: 1,
        size: "M",
      },
    ]);
    session.setRelatedItems([original]);
    session.setRelatedSource(original);
    session.setReaderItem(original);
    session.setSearchResponse({
      matches: { window: [original], archive: [] },
      related: { window: [original], archive: [] },
      semantic_available: true,
    });
    session.replaceItem("same", { hearted: true, signal: 1 });
    for (const copy of [
      session.items()[0],
      session.stories()[0].items[0],
      session.relatedItems()[0],
      session.relatedSource(),
      session.readerItem(),
      session.searchResponse()?.matches.window[0],
      session.searchResponse()?.related.window[0],
    ])
      expect(copy).toMatchObject({ hearted: true, signal: 1, title: "same" });
    session.dispose();
  });
  it("uses the Archive adapter and never polls live Items in Archive mode", async () => {
    const { api, session } = setup();
    session.setMode("archive");
    await session.reload();
    await session.pollNew();
    expect(api.archive).toHaveBeenCalledOnce();
    expect(api.items).not.toHaveBeenCalled();
    session.dispose();
  });
});
describe("Read state", () => {
  it("marks restored Items Read when passed again after clear Undo", async () => {
    vi.useFakeTimers();
    const { api, session, reads } = setup();
    const first = item("one");
    session.setItems([first, item("two")]);
    session.setGridIDs(["one", "two"]);
    const geometry: ReadGeometry = {
      rows: [
        {
          cells: [{ item: first, width: 100, left: 0, effectiveSize: "M" }],
          height: 100,
          top: 0,
          gap: 0,
          kind: "standard",
        },
      ],
      top: 150,
      clientHeight: 200,
      scrollHeight: 1000,
      userInitiated: true,
    };
    reads.onPassed(geometry);
    expect(session.items()[0].read).toBe(true);
    reads.finishAndClear();
    reads.undoLast();
    expect(session.items()[0].read).toBe(false);
    reads.onPassed(geometry);
    expect(session.items()[0].read).toBe(true);
    await reads.flushRead();
    expect(api.readBatch).toHaveBeenCalledWith(["one"], true, false);
    reads.dispose();
    session.dispose();
  });
  it("lets the Reader paint before marking Story members Read", async () => {
    const { api, session, reads } = setup();
    const frames: FrameRequestCallback[] = [];
    vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) =>
      frames.push(callback),
    );
    const story: Story = {
      story_id: "story",
      items: [item("lead"), item("member")],
      source_count: 2,
      order_key: 1,
      size: "M",
    };
    session.setStories([story]);
    reads.openStory(story);
    expect(session.stories()[0].items.every((item) => !item.read)).toBe(true);
    frames.shift()?.(0);
    expect(session.stories()[0].items.every((item) => !item.read)).toBe(true);
    frames.shift()?.(16);
    expect(session.stories()[0].items.every((item) => item.read)).toBe(true);
    await reads.settle();
    expect(api.readBatch).toHaveBeenCalledWith(["lead", "member"], true, false);
    reads.dispose();
    session.dispose();
    vi.unstubAllGlobals();
  });

  it("undoes queued Read without sending a write and restores the grid", async () => {
    vi.useFakeTimers();
    const { api, session, reads } = setup();
    session.setItems([item("one")]);
    session.setGridIDs(["one"]);
    session.setFocusedID("one");
    session.scrollTop = 42;
    reads.finishAndClear(["one"]);
    expect(session.gridIDs()).toEqual([]);
    expect(reads.readAdjust()).toBe(1);
    reads.undoLast();
    await reads.flushRead();
    expect(session.gridIDs()).toEqual(["one"]);
    expect(session.scrollTop).toBe(42);
    expect(session.items()[0].read).toBe(false);
    expect(reads.readAdjust()).toBe(0);
    expect(api.readBatch).not.toHaveBeenCalled();
    reads.dispose();
    session.dispose();
  });
  it("rolls back Read on every Story copy without undoing concurrent Keep", async () => {
    const { api, session, reads } = setup();
    const write = deferred<void>();
    api.readBatch.mockImplementationOnce(() => write.promise);
    const lead = item("lead"),
      member = { ...item("member"), read: true };
    const story: Story = {
      story_id: "story",
      items: [lead, member],
      source_count: 2,
      order_key: 1,
      size: "M",
    };
    session.setStories([story]);
    session.setItems([lead]);
    session.setReaderItem(lead);
    const pending = reads.toggleStoryRead(story);
    session.replaceItem("lead", { hearted: true, signal: 1 });
    await Promise.resolve();
    write.reject(new Error("offline"));
    await pending;
    expect(session.stories()[0].items[0]).toMatchObject({
      read: false,
      hearted: true,
      signal: 1,
    });
    expect(session.readerItem()?.read).toBe(false);
    expect(session.stories()[0].items[1].read).toBe(true);
    expect(reads.readAdjust()).toBe(0);
    reads.dispose();
    session.dispose();
  });
  it("waits for in-flight Read before undo writes Unread", async () => {
    vi.useFakeTimers();
    const { api, session, reads } = setup();
    session.setItems([item("one")]);
    const write = deferred<void>();
    api.readBatch.mockImplementationOnce(() => write.promise);
    reads.queueRead(["one"]);
    const flush = reads.flushRead();
    await Promise.resolve();
    reads.undoLast();
    expect(api.readBatch).toHaveBeenCalledTimes(1);
    write.resolve();
    await flush;
    await reads.settle();
    expect(api.readBatch.mock.calls.map((call) => call[1])).toEqual([
      true,
      false,
    ]);
    expect(session.items()[0].read).toBe(false);
    reads.dispose();
    session.dispose();
  });
  it("does not mark Archive Items Read", () => {
    const { api, session, reads } = setup();
    session.setMode("archive");
    session.setItems([item("kept")]);
    reads.queueRead(["kept"]);
    reads.toggleRead(item("kept"));
    expect(reads.readAdjust()).toBe(0);
    expect(api.read).not.toHaveBeenCalled();
    reads.dispose();
    session.dispose();
  });
});

describe("Grid model", () => {
  it("counts unique unread members and owns finish and cell eligibility", () => {
    const lead = { ...item("lead"), hearted: true };
    const member = { ...item("member"), read: true };
    const story: Story = {
      story_id: "story",
      items: [lead, member],
      source_count: 2,
      order_key: 1,
      size: "M",
    };
    const data = {
      items: [lead],
      stories: [story],
      entries: [{ kind: "story" as const, story }],
      hasMore: false,
      archive: false,
      unreadOnly: true,
      order: "interest" as const,
    };
    const unread = createGridModel(data);
    expect(unread.unreadCount).toBe(1);
    expect(unread.canFinish).toBe(true);
    expect(unread.cell(lead)).toMatchObject({
      signal: true,
      bury: false,
      lifetime: false,
    });
    expect(unread.cell(member).dimmed).toBe(true);
    const all = createGridModel({ ...data, unreadOnly: false });
    expect(all.canFinish).toBe(false);
    expect(all.cell(lead).unreadDot).toBe(true);
    const archive = createGridModel({ ...data, archive: true });
    expect(archive.canFinish).toBe(false);
    expect(archive.cell(lead)).toMatchObject({
      signal: false,
      bury: false,
      lifetime: false,
      dimmed: false,
      unreadDot: false,
    });
  });
});
