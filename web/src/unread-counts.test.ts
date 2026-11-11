import { createRoot, createSignal } from "solid-js";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createItemSession } from "./item-session";
import { createReadState } from "./read-state";
import type {
  Feed,
  FeedItemCounts,
  GridScope,
  Item,
  ItemsResponse,
} from "./types";
import { feedTagOptions } from "./ui/tag-options";
import { createUnreadCounts } from "./unread-counts";

// Read's optimistic unread adjustment, summed over Feeds.
const adjusted = (byFeed: Record<string, number>) =>
  Object.values(byFeed).reduce((sum, value) => sum + value, 0);

const item = (id: string, feed = "feed"): Item => ({
  item_id: id,
  feed_id: feed,
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

const counts = (unread: number, all = unread): FeedItemCounts => ({
  feed: { unread, all },
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

type CountRequest = {
  from?: string;
  reply: ReturnType<typeof deferred<FeedItemCounts>>;
};

const flushMicrotasks = async () => {
  for (let index = 0; index < 20; index++) await Promise.resolve();
};

function setup(options: { scope?: GridScope; feeds?: Feed[] } = {}) {
  const requests: CountRequest[] = [];
  const api = {
    items: vi.fn(
      async (): Promise<ItemsResponse> => ({ items: [], next_cursor: "" }),
    ),
    stories: vi.fn(async () => ({ stories: [] })),
    archive: vi.fn(async () => ({ items: [], next_cursor: "" })),
    read: vi.fn(async (_id: string, _read: boolean) => {}),
    readBatch: vi.fn(
      async (_ids: string[], _read: boolean, _keepalive?: boolean) => {},
    ),
    behaviour: vi.fn(async () => {}),
    feedItemCounts: vi.fn((window?: { from: string; before: string }) => {
      const reply = deferred<FeedItemCounts>();
      requests.push({ from: window?.from, reply });
      return reply.promise;
    }),
  };
  const errors = vi.fn();
  const [feedsScreen, setFeedsScreen] = createSignal(false);
  const [scope] = createSignal<GridScope>(options.scope ?? null);
  const [feeds] = createSignal<Feed[]>(
    options.feeds ?? [{ feed_id: "feed" } as Feed],
  );
  let dispose!: () => void;
  const built = createRoot((rootDispose) => {
    dispose = rootDispose;
    const session = createItemSession(api, {
      onError: errors,
      changed: () => {},
      beforeReload: () => {},
      pagingBlocked: () => false,
      cleared: () => undefined,
      visible: () => true,
    });
    const reads = createReadState(api, session, errors);
    const unread = createUnreadCounts(
      api,
      reads,
      {
        scope,
        itemWindow: session.itemWindow,
        unreadOnly: session.unreadOnly,
        feeds,
        feedsScreen,
        phone: () => false,
      },
      errors,
    );
    return { session, reads, unread };
  });
  const cleanup = () => {
    built.reads.dispose();
    built.session.dispose();
    dispose();
  };
  return { api, errors, requests, setFeedsScreen, cleanup, ...built };
}

afterEach(() => vi.useRealTimers());

describe("Unread counts", () => {
  it("shows refreshed counts and stays unknown while a new Window loads", async () => {
    const { session, unread, requests, cleanup } = setup();
    expect(unread.scopeCell().count).toBeUndefined();
    const first = unread.refresh();
    await flushMicrotasks();
    requests[0].reply.resolve(counts(4));
    await first;
    expect(unread.scopeCell().count).toBe(4);
    session.setItemWindow("today");
    const second = unread.refresh();
    expect(unread.scopeCell().count).toBeUndefined();
    await flushMicrotasks();
    expect(requests[1].from).toBeDefined();
    requests[1].reply.resolve(counts(2));
    await second;
    expect(unread.scopeCell().count).toBe(2);
    cleanup();
  });

  async function loaded(unread = 4) {
    const harness = setup();
    harness.session.setItems([item("one"), item("two")]);
    const first = harness.unread.refresh();
    await flushMicrotasks();
    harness.requests[0].reply.resolve(counts(unread));
    await first;
    return harness;
  }

  it("ignores a delayed reply superseded by a later refresh", async () => {
    const { unread, requests, cleanup } = await loaded();
    const older = unread.refresh();
    await flushMicrotasks();
    const newer = unread.refresh();
    await flushMicrotasks();
    requests[2].reply.resolve(counts(7));
    await newer;
    requests[1].reply.resolve(counts(9));
    await older;
    expect(unread.scopeCell().count).toBe(7);
    cleanup();
  });

  it("ignores a reply for a Window the user already left", async () => {
    const { session, unread, requests, cleanup } = await loaded();
    session.setItemWindow("today");
    const today = unread.refresh();
    await flushMicrotasks();
    session.setItemWindow("yesterday");
    const yesterday = unread.refresh();
    await flushMicrotasks();
    requests[1].reply.resolve(counts(5));
    await today;
    expect(unread.scopeCell().count).toBeUndefined();
    requests[2].reply.resolve(counts(6));
    await yesterday;
    expect(unread.scopeCell().count).toBe(6);
    cleanup();
  });

  it("does not revive old counts for a Window revisited mid-refresh", async () => {
    const { session, unread, requests, cleanup } = await loaded();
    session.setItemWindow("today");
    const today = unread.refresh();
    session.setItemWindow("all");
    expect(unread.scopeCell().count).toBeUndefined();
    await flushMicrotasks();
    requests[1].reply.resolve(counts(5));
    await today;
    cleanup();
  });

  it("flushes queued Read and waits for the write before counting", async () => {
    const { api, reads, unread, requests, cleanup } = await loaded();
    const write = deferred<void>();
    api.readBatch.mockImplementationOnce(() => write.promise);
    reads.queueRead(["one"]);
    expect(unread.scopeCell().count).toBe(3);
    const refresh = unread.refresh();
    await flushMicrotasks();
    expect(api.readBatch).toHaveBeenCalledWith(["one"], true, false);
    expect(requests).toHaveLength(1);
    write.resolve();
    await flushMicrotasks();
    expect(requests).toHaveLength(2);
    expect(unread.scopeCell().count).toBe(3);
    requests[1].reply.resolve(counts(3));
    await refresh;
    expect(adjusted(reads.feedReadAdjust())).toBe(0);
    expect(unread.scopeCell().count).toBe(3);
    cleanup();
  });

  it("waits for an in-flight toggle before counting", async () => {
    const { api, session, reads, unread, requests, cleanup } = await loaded();
    const write = deferred<void>();
    api.read.mockImplementationOnce(() => write.promise);
    void reads.toggleRead(session.items()[0]);
    const refresh = unread.refresh();
    await flushMicrotasks();
    expect(requests).toHaveLength(1);
    write.resolve();
    await flushMicrotasks();
    requests[1].reply.resolve(counts(3));
    await refresh;
    expect(unread.scopeCell().count).toBe(3);
    cleanup();
  });

  it("keeps the optimistic count when a Read write fails", async () => {
    const { api, reads, unread, requests, errors, cleanup } = await loaded();
    api.readBatch.mockRejectedValueOnce(new Error("offline"));
    reads.queueRead(["one"]);
    await unread.refresh();
    expect(errors).toHaveBeenCalled();
    expect(requests).toHaveLength(1);
    expect(adjusted(reads.feedReadAdjust())).toBe(1);
    expect(unread.scopeCell().count).toBe(3);
    cleanup();
  });

  it("rejects counts requested before a concurrent Read, without retrying", async () => {
    const { session, reads, unread, requests, cleanup } = await loaded();
    const refresh = unread.refresh();
    await flushMicrotasks();
    void reads.toggleRead(session.items()[0]);
    expect(unread.scopeCell().count).toBe(3);
    requests[1].reply.resolve(counts(4));
    await refresh;
    await flushMicrotasks();
    expect(requests).toHaveLength(2);
    expect(adjusted(reads.feedReadAdjust())).toBe(1);
    expect(unread.scopeCell().count).toBe(3);
    cleanup();
  });

  it("rejects counts requested before a concurrent Undo", async () => {
    const { reads, unread, requests, cleanup } = await loaded();
    reads.queueRead(["one"]);
    const refresh = unread.refresh();
    await flushMicrotasks();
    expect(requests).toHaveLength(2);
    reads.undoLast();
    expect(unread.scopeCell().count).toBe(4);
    requests[1].reply.resolve(counts(3));
    await refresh;
    expect(unread.scopeCell().count).toBe(4);
    cleanup();
  });

  it("keeps same-Window counts when counts are missing or fail", async () => {
    const { reads, unread, requests, errors, cleanup } = await loaded();
    reads.queueRead(["one"]);
    const missing = unread.refresh();
    await flushMicrotasks();
    requests[1].reply.resolve(undefined as unknown as FeedItemCounts);
    await missing;
    expect(unread.scopeCell().count).toBe(3);
    const failed = unread.refresh();
    await flushMicrotasks();
    requests[2].reply.reject(new Error("offline"));
    await failed;
    expect(errors).toHaveBeenCalledTimes(1);
    expect(adjusted(reads.feedReadAdjust())).toBe(1);
    expect(unread.scopeCell().count).toBe(3);
    cleanup();
  });

  it("stays unknown for a changed Window whose counts fail", async () => {
    const { session, unread, requests, errors, cleanup } = await loaded();
    session.setItemWindow("today");
    const failed = unread.refresh();
    await flushMicrotasks();
    requests[1].reply.reject(new Error("offline"));
    await failed;
    expect(errors).toHaveBeenCalledTimes(1);
    expect(unread.ready()).toBe(false);
    expect(unread.scopeCell().count).toBeUndefined();
    cleanup();
  });

  it("loads each alternate Window once and drops them on refresh", async () => {
    const { api, unread, requests, cleanup } = await loaded();
    api.feedItemCounts.mockClear();
    unread.loadWindowChoices();
    unread.loadWindowChoices();
    expect(api.feedItemCounts).toHaveBeenCalledTimes(2);
    const [today, yesterday] = requests.slice(1);
    today.reply.resolve(counts(2));
    await flushMicrotasks();
    expect(unread.windowChoices(4)).toEqual({
      all: 4,
      today: 2,
      yesterday: undefined,
    });
    const refresh = unread.refresh();
    expect(unread.windowChoices(4).today).toBeUndefined();
    yesterday.reply.resolve(counts(1));
    await flushMicrotasks();
    expect(unread.windowChoices(4).yesterday).toBeUndefined();
    unread.loadWindowChoices();
    expect(api.feedItemCounts).toHaveBeenCalledTimes(5);
    requests[3].reply.resolve(counts(4));
    await refresh;
    cleanup();
  });

  it("gives the Feeds screen All counts only", async () => {
    const { session, unread, requests, setFeedsScreen, cleanup } = setup();
    session.setItemWindow("today");
    setFeedsScreen(true);
    expect(unread.feedsScreenCounts()).toBeUndefined();
    const refresh = unread.refresh();
    await flushMicrotasks();
    expect(requests[0].from).toBeUndefined();
    requests[0].reply.resolve(counts(8));
    await refresh;
    expect(unread.feedsScreenCounts()).toEqual(counts(8));
    expect(unread.ready()).toBe(false);
    cleanup();
  });

  describe("per Feed", () => {
    async function twoFeeds() {
      const harness = setup();
      harness.session.setItems([
        item("apple", "apple-feed"),
        item("art", "art-feed"),
      ]);
      const first = harness.unread.refresh();
      await flushMicrotasks();
      harness.requests[0].reply.resolve({
        "apple-feed": { unread: 1, all: 3 },
        "art-feed": { unread: 2, all: 5 },
      });
      await first;
      return harness;
    }

    it("lowers a Feed's unread count as soon as its Item is read", async () => {
      const { reads, unread, cleanup } = await twoFeeds();
      reads.queueRead(["apple"]);
      expect(unread.feedCounts()).toEqual({
        "apple-feed": { unread: 0, all: 3 },
        "art-feed": { unread: 2, all: 5 },
      });
      cleanup();
    });

    it("hands the Read over to the settled server counts", async () => {
      const { reads, unread, requests, cleanup } = await twoFeeds();
      reads.queueRead(["apple"]);
      const refresh = unread.refresh();
      await flushMicrotasks();
      requests[1].reply.resolve({
        "apple-feed": { unread: 0, all: 3 },
        "art-feed": { unread: 2, all: 5 },
      });
      await refresh;
      expect(unread.feedCounts()["apple-feed"]).toEqual({ unread: 0, all: 3 });
      cleanup();
    });

    it("restores a Feed's unread count on Undo", async () => {
      const { reads, unread, cleanup } = await twoFeeds();
      reads.queueRead(["apple", "art"]);
      reads.undoLast();
      expect(unread.feedCounts()).toEqual({
        "apple-feed": { unread: 1, all: 3 },
        "art-feed": { unread: 2, all: 5 },
      });
      cleanup();
    });

    it("follows a toggled Item and rolls back when its write fails", async () => {
      const { api, session, reads, unread, cleanup } = await twoFeeds();
      const write = deferred<void>();
      api.read.mockImplementationOnce(() => write.promise);
      const toggle = reads.toggleRead(session.items()[0]);
      expect(unread.feedCounts()["apple-feed"]).toEqual({ unread: 0, all: 3 });
      write.reject(new Error("offline"));
      await toggle;
      expect(unread.feedCounts()["apple-feed"]).toEqual({ unread: 1, all: 3 });
      cleanup();
    });

    it("lowers the Feed of an Item marked below from an unloaded page", async () => {
      const { api, session, reads, unread, requests, cleanup } = setup();
      api.items.mockResolvedValueOnce({
        items: [item("apple", "apple-feed"), item("art", "art-feed")],
        next_cursor: "later",
      });
      await session.reload();
      const first = unread.refresh();
      await flushMicrotasks();
      requests[0].reply.resolve({
        "apple-feed": { unread: 1, all: 1 },
        "art-feed": { unread: 1, all: 1 },
        "news-feed": { unread: 1, all: 1 },
      });
      await first;
      api.items.mockResolvedValueOnce({
        items: [item("news", "news-feed")],
        next_cursor: "",
      });
      await reads.markBelow("apple");
      expect(unread.feedCounts()).toEqual({
        "apple-feed": { unread: 0, all: 1 },
        "art-feed": { unread: 0, all: 1 },
        "news-feed": { unread: 0, all: 1 },
      });
      cleanup();
    });
  });

  describe("Scope cell", () => {
    const feeds = [
      { feed_id: "apple-feed", tags: ["fruit"] },
      { feed_id: "art-feed", tags: ["culture"] },
    ] as Feed[];

    async function scoped(scope: GridScope, feedList = feeds) {
      const harness = setup({ scope, feeds: feedList });
      harness.session.setItems([
        item("apple", "apple-feed"),
        item("art", "art-feed"),
      ]);
      const first = harness.unread.refresh();
      await flushMicrotasks();
      harness.requests[0].reply.resolve({
        "apple-feed": { unread: 1, all: 3 },
        "art-feed": { unread: 2, all: 5 },
      });
      await first;
      return harness;
    }

    it("ignores a Read in a Feed outside the Tag Scope", async () => {
      const { reads, unread, cleanup } = await scoped({
        kind: "tag",
        value: "fruit",
      });
      reads.queueRead(["art"]);
      expect(unread.scopeCell().count).toBe(1);
      cleanup();
    });

    it("ignores a Read in another Feed under a Feed Scope", async () => {
      const { reads, unread, cleanup } = await scoped({
        kind: "feed",
        value: "art-feed",
      });
      reads.queueRead(["apple"]);
      expect(unread.scopeCell().count).toBe(2);
      cleanup();
    });

    it("ignores a Read in a muted Feed with no Scope", async () => {
      const { reads, unread, cleanup } = await scoped(null, [
        feeds[0],
        { ...feeds[1], muted: true },
      ]);
      reads.queueRead(["art"]);
      expect(unread.scopeCell().count).toBe(1);
      cleanup();
    });

    it("lowers for a Read in the Scope and restores on Undo", async () => {
      const { reads, unread, cleanup } = await scoped({
        kind: "tag",
        value: "culture",
      });
      reads.queueRead(["art"]);
      expect(unread.scopeCell().count).toBe(1);
      reads.undoLast();
      expect(unread.scopeCell().count).toBe(2);
      cleanup();
    });

    it("restores a toggled Read in the Scope when its write fails", async () => {
      const { api, session, reads, unread, cleanup } = await scoped({
        kind: "tag",
        value: "fruit",
      });
      const write = deferred<void>();
      api.read.mockImplementationOnce(() => write.promise);
      const toggle = reads.toggleRead(session.items()[0]);
      expect(unread.scopeCell().count).toBe(0);
      write.reject(new Error("offline"));
      await toggle;
      expect(unread.scopeCell().count).toBe(1);
      cleanup();
    });

    it("agrees with the Tag chip while Reads are optimistic", async () => {
      const both = feeds.map((feed) => ({
        ...feed,
        tags: [...(feed.tags ?? []), "daily"],
      }));
      const { reads, unread, cleanup } = await scoped(
        { kind: "tag", value: "daily" },
        both,
      );
      reads.queueRead(["apple", "art"]);
      const chip = feedTagOptions(both, unread.feedCounts(), true).find(
        (option) => option.tag === "daily",
      );
      expect(chip?.count).toBe(1);
      expect(unread.scopeCell().count).toBe(1);
      // apple-feed has nothing left unread, so only art-feed contributes.
      expect(unread.scopeCell().text).toBe("1 unread item · 1 feed");
      cleanup();
    });
  });
});
