import {
  createMemo,
  createResource,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import { type APIClient, APIError } from "../api/client";
import { archiveSize } from "../archive";
import { AppMark } from "../components/AppMark";
import { Icon, type IconName } from "../components/Icon";
import {
  brokenSince,
  compareFeeds,
  type FeedFilter,
  type FeedSort,
  feedFilterCounts,
  filterFeeds,
} from "../feed-manager";
import { formatPrior } from "../ranking-display";
import {
  type RedditCollection,
  redditCanonicalURL,
  redditCollectionFromURL,
  redditCollectionLabel,
  redditSubreddit,
} from "../reddit-feed";
import type { Feed, FeedCandidate, FeedItemCounts } from "../types";
import {
  type DiscoveryState,
  discoveredCandidateState,
} from "./feed-discovery";
import { upsertFeed } from "./feed-list";
import { relativeTime } from "./Grid";
import { type BadgeSize, SourceBadge } from "./SourceBadge";
import { displayFeedTitle as displayTitle } from "./tag-options";

export function Feeds(props: {
  api: APIClient;
  itemCounts: FeedItemCounts;
  onRefreshCounts(): Promise<void>;
  focusSearch?: boolean;
  heartCount: number;
  onBack(): void;
  onKeys(): void;
  characterShortcuts?: boolean;
  onCharacterShortcuts?(enabled: boolean): void;
  onSignOut(): void;
  onFeedsChanged?(): void;
  onToast(kind: "success" | "error", message: string): void;
}) {
  const [feeds, { refetch, mutate: setFeeds }] = createResource(() =>
    props.api.feeds(),
  );
  const [account, { refetch: refetchAccount, mutate: setAccount }] =
    createResource(() => props.api.me());
  const [recentCounts] = createResource(async () => {
    const now = Date.now();
    try {
      return await props.api.feedItemCounts({
        from: new Date(now - 30 * 86400000).toISOString(),
        before: new Date(now).toISOString(),
      });
    } catch {
      props.onToast("error", "Couldn’t load recent feed counts");
      return {};
    }
  });
  const [filter, setFilter] = createSignal<FeedFilter>("all");
  const counts = createMemo(() =>
    feedFilterCounts(feeds() ?? [], recentCounts()),
  );
  const toggleFilter = (next: FeedFilter) =>
    setFilter(filter() === next ? "all" : next);
  const [query, setQuery] = createSignal("");
  const [sort, setSort] = createSignal<FeedSort>("title");
  const [selectedID, setSelectedID] = createSignal("");
  const [adding, setAdding] = createSignal(false);
  const [message, setMessage] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [recomputing, setRecomputing] = createSignal(false);
  const [undoFeeds, setUndoFeeds] = createSignal<Feed[]>([]);
  const [triageBusy, setTriageBusy] = createSignal(false);
  const [confirmRemoval, setConfirmRemoval] = createSignal<Feed[]>([]);
  const longBroken = createMemo(() => {
    const now = Date.now();
    return (feeds() ?? []).filter((feed) => {
      const since = brokenSince(feed, now);
      return since !== undefined && now - Date.parse(since) >= 7 * 86400000;
    });
  });
  let undoTimer: number | undefined;
  let input!: HTMLInputElement;
  let searchInput!: HTMLInputElement;

  onMount(() => {
    if (Object.keys(props.itemCounts).length === 0)
      void props.onRefreshCounts();
    if (props.focusSearch) searchInput.focus();
  });

  const selected = createMemo(() =>
    feeds()?.find((feed) => feed.feed_id === selectedID()),
  );
  const allTags = createMemo(() =>
    [...new Set((feeds() ?? []).flatMap((feed) => feed.tags ?? []))].sort(),
  );
  const attention = createMemo(
    () =>
      feeds()?.filter(
        (feed) => feed.status === "broken" || feed.status === "slowed",
      ).length ?? 0,
  );
  const visibleFeeds = createMemo(() =>
    filterFeeds(feeds() ?? [], filter(), query(), recentCounts()).sort(
      compareFeeds(sort(), props.itemCounts, recentCounts()),
    ),
  );

  const refresh = async () => {
    await refetch();
    props.onFeedsChanged?.();
  };

  const importFile = async (file?: File) => {
    if (!file) return;
    setBusy(true);
    setMessage("");
    try {
      const result = await props.api.importOPML(file);
      setMessage(
        `${result.imported} ${result.imported === 1 ? "feed" : "feeds"} imported.`,
      );
      await refresh();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Import failed.");
    } finally {
      setBusy(false);
      input.value = "";
    }
  };

  const exportOPML = async () => {
    setBusy(true);
    try {
      const blob = await props.api.exportOPML();
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = href;
      anchor.download = "sema-feeds.opml";
      anchor.click();
      URL.revokeObjectURL(href);
      props.onToast("success", "OPML exported");
    } catch {
      props.onToast("error", "Couldn’t export OPML");
    } finally {
      setBusy(false);
    }
  };

  const offerUndo = (removed: Feed[]) => {
    if (!removed.length) return;
    setUndoFeeds((current) => [...current, ...removed]);
    window.clearTimeout(undoTimer);
    undoTimer = window.setTimeout(() => setUndoFeeds([]), 8_000);
  };

  const remove = async (feed: Feed) => {
    await props.api.deleteFeed(feed.feed_id);
    setSelectedID("");
    offerUndo([feed]);
    await refresh();
  };

  const undoRemove = async (removed: Feed[]) => {
    if (!removed.length) return;
    setUndoFeeds([]);
    window.clearTimeout(undoTimer);
    const results = await Promise.allSettled(
      removed.map(async (feed) => {
        const restored = await props.api.addFeed({
          feed_url: feed.url,
          tags: feed.tags,
          custom_title: feed.custom_title,
          connector: feed.connector,
          title: feed.title,
          site_url: feed.site_url,
          badge_url: feed.connector === "reddit" ? feed.favicon_url : undefined,
          avatar_url:
            feed.connector === "youtube" ? feed.favicon_url : undefined,
        });
        if (
          feed.muted ||
          feed.hide_shorts ||
          feed.always_generate ||
          (feed.connector !== "reddit" && feed.fetch_interval_h !== 1)
        ) {
          await props.api.patchFeed(restored.feed.feed_id, {
            muted: feed.muted,
            hide_shorts:
              feed.connector === "youtube" ? feed.hide_shorts : undefined,
            always_generate: feed.always_generate,
            fetch_interval_h:
              feed.connector === "reddit" ? undefined : feed.fetch_interval_h,
          });
        }
      }),
    );
    const failed = results.filter(
      (result) => result.status === "rejected",
    ).length;
    try {
      await refresh();
      props.onToast(
        failed ? "error" : "success",
        failed
          ? `Couldn’t fully restore ${failed} ${failed === 1 ? "feed" : "feeds"}`
          : removed.length === 1
            ? "Feed restored"
            : `${removed.length} feeds restored`,
      );
    } catch {
      props.onToast("error", "Couldn’t refresh feeds after restoring");
    }
  };

  const retryLongBroken = async () => {
    if (triageBusy()) return;
    const targets = [...longBroken()];
    setTriageBusy(true);
    setConfirmRemoval([]);
    props.onToast(
      "success",
      `Retrying ${targets.length} ${targets.length === 1 ? "feed" : "feeds"}`,
    );
    try {
      let rejected = 0;
      // Settle each request before starting the next, continuing after failures.
      for (const feed of targets) {
        const [result] = await Promise.allSettled([
          props.api.retryFeed(feed.feed_id),
        ]);
        if (result.status === "rejected") rejected++;
      }
      const latest = await refetch();
      props.onFeedsChanged?.();
      const ids = new Set(targets.map((feed) => feed.feed_id));
      const failing = (latest ?? []).filter(
        (feed) =>
          ids.has(feed.feed_id) &&
          (feed.status === "broken" || feed.status === "slowed"),
      ).length;
      props.onToast(
        failing || rejected ? "error" : "success",
        rejected
          ? `${failing} ${failing === 1 ? "feed" : "feeds"} still failing · Couldn’t retry ${rejected}`
          : failing
            ? `${failing} ${failing === 1 ? "feed" : "feeds"} still failing`
            : "All feeds recovered",
      );
    } catch {
      props.onToast("error", "Couldn’t refresh feeds after retrying");
    } finally {
      setTriageBusy(false);
    }
  };

  const removeLongBroken = async () => {
    if (triageBusy()) return;
    const targets = [...confirmRemoval()];
    setTriageBusy(true);
    const removed: Feed[] = [];
    try {
      for (const feed of targets) {
        const [result] = await Promise.allSettled([
          props.api.deleteFeed(feed.feed_id),
        ]);
        if (result.status === "fulfilled") removed.push(feed);
      }
      offerUndo(removed);
      const ids = new Set(removed.map((feed) => feed.feed_id));
      setFeeds((current) => current?.filter((feed) => !ids.has(feed.feed_id)));
      if (ids.has(selectedID())) setSelectedID("");
      const failed = targets.length - removed.length;
      if (failed)
        props.onToast(
          "error",
          `Couldn’t remove ${failed} ${failed === 1 ? "feed" : "feeds"}`,
        );
      await refresh();
    } catch {
      props.onToast("error", "Couldn’t refresh feeds after removing");
    } finally {
      setConfirmRemoval([]);
      setTriageBusy(false);
    }
  };

  const recompute = async () => {
    if (recomputing()) return;
    setRecomputing(true);
    try {
      const result = await props.api.recomputeRanking();
      setAccount((current) =>
        current ? { ...current, model: result.model } : current,
      );
      await Promise.all([refetch(), refetchAccount()]);
      props.onToast("success", "Ranking updated");
    } catch {
      props.onToast("error", "Couldn't recompute");
    } finally {
      setRecomputing(false);
    }
  };

  onCleanup(() => window.clearTimeout(undoTimer));

  return (
    <main
      class="feeds-view"
      onKeyDown={(event) => {
        if (
          event.key !== "Escape" ||
          !(event.target instanceof HTMLElement) ||
          !event.target.matches("input, textarea, select")
        )
          return;
        event.preventDefault();
        event.stopPropagation();
        event.target.blur();
      }}
    >
      <header class="feeds-header">
        <AppMark onActivate={props.onBack} />
        <span>/ feeds &amp; settings</span>
        <span class="feeds-summary">
          {feeds()?.length ?? 0} feeds · {attention()} need attention
        </span>
        <button type="button" class="keys-chip" onClick={props.onKeys}>
          ? keys
        </button>
        <button type="button" class="signout" onClick={props.onSignOut}>
          sign out
        </button>
      </header>

      <section class="feed-manager">
        <div class="feed-manager-title">
          <h1>Feeds</h1>
          <p>
            {feeds()?.length ?? 0} feeds ·{" "}
            <button type="button" onClick={() => setFilter("attention")}>
              {attention()} need attention
            </button>
          </p>
        </div>
        <div class="feed-toolbar">
          <label class="feed-search">
            <Icon name="search" />
            <input
              type="search"
              ref={searchInput}
              value={query()}
              placeholder="Search feeds"
              aria-label="Search feeds"
              onInput={(event) => setQuery(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key !== "Escape") return;
                event.preventDefault();
                event.stopPropagation();
                setQuery("");
                event.currentTarget.blur();
              }}
            />
            <Show when={query()}>
              <small>{visibleFeeds().length}</small>
            </Show>
          </label>
          <label class="feed-sort">
            <span class="sr-only">Sort feeds</span>
            <Icon name="sort" class="sort-icon" />
            <select
              value={sort()}
              onChange={(event) =>
                setSort(event.currentTarget.value as FeedSort)
              }
            >
              <option value="title">Title (A–Z)</option>
              <option value="unread">Unread</option>
              <option value="quietest">Quietest</option>
              <option value="updated">Last update</option>
              <option value="errors">Errors first</option>
              <option value="prior">Prior</option>
              <option value="quality">Extraction quality</option>
            </select>
            <Icon name="chevron-down" class="sort-menu-icon" />
          </label>
          <button
            type="button"
            class="feed-add"
            aria-label="Add feed"
            onClick={() => setAdding(true)}
          >
            <Icon name="add-feed" />
          </button>
        </div>

        <Show when={longBroken().length > 0}>
          <section
            class="feed-triage"
            aria-label="Long-broken feeds"
            aria-busy={triageBusy()}
          >
            <p>
              {longBroken().length}{" "}
              {longBroken().length === 1 ? "feed has" : "feeds have"} been
              failing for over a week.
            </p>
            <div>
              <button
                type="button"
                onClick={() => {
                  setFilter("attention");
                  setSort("errors");
                }}
              >
                Show
              </button>
              <button
                type="button"
                disabled={triageBusy()}
                onClick={retryLongBroken}
              >
                Retry all
              </button>
              <Show
                when={confirmRemoval().length > 0}
                fallback={
                  <button
                    type="button"
                    disabled={triageBusy()}
                    onClick={() => setConfirmRemoval([...longBroken()])}
                  >
                    Remove all
                  </button>
                }
              >
                <span>
                  Remove {confirmRemoval().length}{" "}
                  {confirmRemoval().length === 1 ? "feed" : "feeds"}?
                </span>
                <button
                  type="button"
                  disabled={triageBusy()}
                  onClick={removeLongBroken}
                >
                  Confirm removal
                </button>
                <button
                  type="button"
                  disabled={triageBusy()}
                  onClick={() => setConfirmRemoval([])}
                >
                  Cancel
                </button>
              </Show>
            </div>
          </section>
        </Show>
        <section class="feed-filters" aria-label="Filter feeds">
          <For
            each={
              [
                ["all", "All"],
                ["attention", "Needs attention"],
                ["muted", "Muted"],
                ["never", "Never fetched"],
                ["quiet", "Quiet"],
              ] as const
            }
          >
            {([value, label]) => (
              <Show when={value === "all" || counts()[value] > 0}>
                <button
                  type="button"
                  class="tag-chip"
                  aria-pressed={filter() === value}
                  onClick={() => toggleFilter(value)}
                >
                  {label} <span>{counts()[value]}</span>
                </button>
              </Show>
            )}
          </For>
          <Show when={allTags().length}>
            <section class="feed-filter-tags" aria-label="Tags">
              <For each={allTags()}>
                {(tag) => (
                  <button
                    type="button"
                    class="tag-chip"
                    aria-pressed={filter() === `tag:${tag}`}
                    onClick={() => toggleFilter(`tag:${tag}`)}
                  >
                    {tag} <span>{counts().tags[tag]}</span>
                  </button>
                )}
              </For>
            </section>
          </Show>
        </section>

        <div class="feed-manage-list" aria-busy={feeds.loading}>
          <Show
            when={!feeds.loading}
            fallback={<p class="feed-empty">Loading feeds…</p>}
          >
            <For
              each={visibleFeeds()}
              fallback={
                <div class="feed-empty">
                  <p>
                    {filter() !== "all"
                      ? "No feeds match this filter."
                      : query()
                        ? "No feeds match."
                        : "No feeds yet."}
                  </p>
                  <Show
                    when={filter() !== "all"}
                    fallback={
                      <button type="button" onClick={() => setAdding(true)}>
                        <Icon name="add-feed" />
                        {query() ? "Add it as a feed?" : "Add your first feed"}
                      </button>
                    }
                  >
                    <button type="button" onClick={() => setFilter("all")}>
                      Clear filter
                    </button>
                  </Show>
                </div>
              }
            >
              {(feed) => (
                <button
                  type="button"
                  class="feed-manage-row"
                  classList={{ muted: feed.muted }}
                  aria-label={`${displayTitle(feed)}, ${feed.status} feed`}
                  onClick={() => setSelectedID(feed.feed_id)}
                >
                  <FeedIcon feed={feed} />
                  <div class="feed-row-copy">
                    <div>
                      <strong>{displayTitle(feed)}</strong>
                      <For each={(feed.tags ?? []).slice(0, 3)}>
                        {(tag) => <span class="tag-chip">{tag}</span>}
                      </For>
                    </div>
                    <small>
                      {feedDescriptor(feed)}
                      {!feed.muted && recentCounts()?.[feed.feed_id]?.all === 0
                        ? " · 0 in 30 days"
                        : ""}
                    </small>
                  </div>
                  <span
                    class="feed-unread"
                    classList={{
                      zero: props.itemCounts[feed.feed_id]?.unread === 0,
                    }}
                    title="Unread items"
                  >
                    {props.itemCounts[feed.feed_id]?.unread}
                  </span>
                  <span class="feed-row-status">
                    <StatusBadge feed={feed} />
                  </span>
                  <span class="feed-row-prior">
                    <Show when={!feed.muted}>
                      <PriorBadge feed={feed} />
                    </Show>
                  </span>
                  <time
                    title={
                      brokenSince(feed)
                        ? `${new Date(brokenSince(feed) ?? "").toLocaleString()}${feed.last_error ? ` · ${feed.last_error}` : ""}`
                        : undefined
                    }
                  >
                    {brokenSince(feed)
                      ? `failing ${relativeTime(brokenSince(feed) ?? "")}`
                      : feed.muted
                        ? "paused"
                        : feed.last_fetch_at
                          ? `${relativeTime(feed.last_fetch_at)} ago`
                          : "never"}
                  </time>
                </button>
              )}
            </For>
          </Show>
        </div>

        <div class="feed-transfer">
          <div>
            <strong>OPML</strong>
            <span>Move titles, tags, mute state, and intervals.</span>
          </div>
          <input
            ref={input}
            type="file"
            accept=".opml,.xml,text/xml,application/xml"
            onChange={(event) => importFile(event.currentTarget.files?.[0])}
          />
          <button type="button" disabled={busy()} onClick={() => input.click()}>
            <Icon name="import-opml" />
            Import
          </button>
          <button type="button" disabled={busy()} onClick={exportOPML}>
            <Icon name="export-opml" />
            Export
          </button>
        </div>
        <Show when={message()}>
          <p class="form-message" role="status">
            {message()}
          </p>
        </Show>

        <section class="keyboard-preference" aria-label="Keyboard settings">
          <h2>Keyboard</h2>
          <label>
            <input
              type="checkbox"
              checked={props.characterShortcuts ?? true}
              onChange={(event) =>
                props.onCharacterShortcuts?.(event.currentTarget.checked)
              }
            />
            Letter and symbol shortcuts
          </label>
          <p>
            Use letters and sequences such as g → t. Turn off for speech input.
            Arrow keys, Tab, Enter and Escape still work.
          </p>
          <button type="button" onClick={props.onKeys}>
            View keyboard shortcuts
          </button>
        </section>

        <details class="feed-operations">
          <summary>Ranking &amp; storage</summary>
          <div>
            <span>
              {formatCount(account()?.model.explicit_count ?? 0)} explicit ·{" "}
              {formatCount(account()?.model.implicit_count ?? 0)} implicit
            </span>
            <span>
              {props.heartCount} kept · ~{archiveSize(props.heartCount)}
            </span>
            <button
              type="button"
              disabled={recomputing()}
              aria-busy={recomputing()}
              onClick={recompute}
            >
              {recomputing() ? "Recomputing…" : "Recompute ranking"}
            </button>
          </div>
        </details>
      </section>

      <Show when={undoFeeds().length > 0}>
        <div class="feed-undo" role="status">
          <span>
            {undoFeeds().length === 1
              ? "Feed removed"
              : `${undoFeeds().length} feeds removed`}
          </span>
          <button type="button" onClick={() => void undoRemove(undoFeeds())}>
            Undo
          </button>
        </div>
      </Show>
      <Show when={selected()} keyed>
        {(feed) => (
          <FeedDrawer
            api={props.api}
            feed={feed}
            allTags={allTags()}
            onClose={() => setSelectedID("")}
            onChanged={refresh}
            onRemove={() => remove(feed)}
            onToast={props.onToast}
          />
        )}
      </Show>
      <Show when={adding()}>
        <AddFeedDialog
          api={props.api}
          allTags={allTags()}
          onClose={() => setAdding(false)}
          onAdded={(feed) => {
            setFeeds((current) => upsertFeed(current, feed));
            setAdding(false);
            props.onFeedsChanged?.();
          }}
          onToast={props.onToast}
        />
      </Show>
    </main>
  );
}

function FeedDrawer(props: {
  api: APIClient;
  feed: Feed;
  allTags: string[];
  onClose(): void;
  onChanged(): Promise<void>;
  onRemove(): Promise<void>;
  onToast(kind: "success" | "error", message: string): void;
}) {
  const [retrying, setRetrying] = createSignal(false);
  const [confirming, setConfirming] = createSignal(false);
  const [working, setWorking] = createSignal(false);
  let panel!: HTMLElement;
  let name: HTMLInputElement | undefined;

  onMount(() => {
    (name ?? panel.querySelector<HTMLElement>(focusSelector))?.focus();
    const dismiss = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      if (confirming()) setConfirming(false);
      else props.onClose();
    };
    window.addEventListener("keydown", dismiss, true);
    onCleanup(() => window.removeEventListener("keydown", dismiss, true));
  });

  const patch = async (
    value: Partial<
      Pick<
        Feed,
        | "custom_title"
        | "tags"
        | "muted"
        | "hide_shorts"
        | "always_generate"
        | "fetch_interval_h"
        | "url"
      >
    >,
  ) => {
    setWorking(true);
    try {
      await props.api.patchFeed(props.feed.feed_id, value);
      await props.onChanged();
    } catch {
      props.onToast("error", "Couldn’t update feed");
    } finally {
      setWorking(false);
    }
  };

  const retry = async () => {
    setRetrying(true);
    try {
      await props.api.retryFeed(props.feed.feed_id);
      await props.onChanged();
      props.onToast("success", "Feed queued for retry");
    } catch {
      props.onToast("error", "Couldn’t retry feed");
    } finally {
      setRetrying(false);
    }
  };

  const trap = (event: KeyboardEvent) => {
    if (event.key !== "Tab") return;
    const controls = [
      ...panel.querySelectorAll<HTMLElement>(focusSelector),
    ].filter((element) => !element.hasAttribute("disabled"));
    if (controls.length === 0) return;
    const first = controls[0];
    const last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  };

  return (
    <div class="feed-drawer-layer">
      <aside
        ref={panel}
        class="feed-drawer"
        role="dialog"
        aria-modal="true"
        aria-labelledby="feed-drawer-title"
        onKeyDown={trap}
      >
        <button
          type="button"
          class="drawer-close"
          aria-label="Close feed details"
          onClick={props.onClose}
        >
          <Icon name="close" />
        </button>
        <header>
          <FeedIcon feed={props.feed} size={32} />
          <div>
            <div>
              <h2 id="feed-drawer-title">{displayTitle(props.feed)}</h2>
              <DrawerStatus feed={props.feed} />
            </div>
            <code class="feed-source-url">{props.feed.url}</code>
            <Show when={props.feed.site_url}>
              {(siteURL) => (
                <a href={siteURL()} target="_blank" rel="noreferrer">
                  Visit site
                  <Icon name="open-original" />
                </a>
              )}
            </Show>
            <small>{drawerDescriptor(props.feed)}</small>
          </div>
        </header>
        <Show when={props.feed.muted}>
          <p class="paused-copy">
            paused · {props.feed.item_count} items ingested
          </p>
        </Show>

        <label class="drawer-field">
          <span>NAME</span>
          <input
            ref={name}
            value={props.feed.custom_title ?? ""}
            placeholder={props.feed.title || domainName(props.feed.url)}
            onBlur={(event) => {
              const next = event.currentTarget.value.trim();
              if (next !== (props.feed.custom_title ?? ""))
                void patch({ custom_title: next });
            }}
          />
        </label>
        <div class="drawer-field">
          <span>TAGS</span>
          <TagEditor
            tags={props.feed.tags ?? []}
            suggestions={props.allTags}
            onChange={(tags) => void patch({ tags })}
          />
        </div>

        <Show
          when={props.feed.connector === "reddit"}
          fallback={
            <>
              <fieldset
                class="drawer-field interval-field"
                disabled={props.feed.muted || working()}
              >
                <legend>FETCH EVERY</legend>
                <div>
                  <For each={[1, 6, 24] as const}>
                    {(interval) => (
                      <button
                        type="button"
                        classList={{
                          active: props.feed.fetch_interval_h === interval,
                        }}
                        onClick={() =>
                          void patch({ fetch_interval_h: interval })
                        }
                      >
                        {interval}h
                      </button>
                    )}
                  </For>
                </div>
              </fieldset>
              <section class="drawer-content">
                <h3>CONTENT</h3>
                <div class="content-row extraction-row">
                  <span>
                    <strong>Extraction</strong>
                    <small>Last 30 days</small>
                  </span>
                  <ExtractionQuality feed={props.feed} />
                </div>
                <label class="content-row generate-row">
                  <span>
                    <strong>Always generate summaries</strong>
                    <small>
                      {props.feed.always_generate
                        ? "On for this feed · adds ~2s to first open"
                        : "Even when the body extracts cleanly"}
                    </small>
                  </span>
                  <input
                    type="checkbox"
                    checked={props.feed.always_generate}
                    disabled={working()}
                    onChange={(event) =>
                      void patch({
                        always_generate: event.currentTarget.checked,
                      })
                    }
                  />
                  <i />
                </label>
              </section>
            </>
          }
        >
          <fieldset
            class="reddit-collect drawer-reddit-collect"
            disabled={props.feed.muted || working()}
          >
            <legend>
              <strong>Collect</strong>
              <small>Which listing Sema fetches</small>
            </legend>
            <RedditCollectionControl
              value={redditCollectionFromURL(props.feed.url)}
              onChange={(collection) =>
                void patch({
                  url: redditCanonicalURL(props.feed.url, collection),
                })
              }
            />
          </fieldset>
        </Show>
        <MuteControl
          feed={props.feed}
          working={working()}
          onChange={(muted) => void patch({ muted })}
        />
        <Show when={props.feed.connector === "youtube"}>
          <label class="mute-row shorts-row">
            <Icon name="play" />
            <span>
              <strong>Hide Shorts</strong>
              <small>Skip vertical uploads under 60s</small>
            </span>
            <input
              type="checkbox"
              checked={props.feed.hide_shorts}
              disabled={working()}
              onChange={(event) =>
                void patch({ hide_shorts: event.currentTarget.checked })
              }
            />
            <i />
          </label>
        </Show>
        <Show when={props.feed.last_error}>
          <div class="last-error">
            <span>LAST ERROR</span>
            <code>{props.feed.last_error}</code>
          </div>
        </Show>

        <div class="drawer-actions" aria-live="polite">
          <Show
            when={!confirming()}
            fallback={
              <div class="remove-confirm">
                <strong>
                  {props.feed.connector === "reddit"
                    ? "Unsubscribe from subreddit?"
                    : "Remove feed?"}
                </strong>
                <p>
                  Recent items from this feed will disappear. Kept items stay in
                  the archive.
                </p>
                <div>
                  <button type="button" onClick={() => setConfirming(false)}>
                    Cancel
                  </button>
                  <button
                    type="button"
                    class="remove"
                    autofocus
                    onClick={() => void props.onRemove()}
                  >
                    <Icon name="remove-feed" />
                    {props.feed.connector === "reddit"
                      ? "Unsubscribe"
                      : "Remove"}
                  </button>
                </div>
              </div>
            }
          >
            <button
              type="button"
              class="retry"
              disabled={props.feed.muted || retrying()}
              aria-busy={retrying()}
              onClick={retry}
            >
              <Show when={retrying()}>
                <i aria-hidden="true" />
              </Show>
              <Show when={!retrying()}>
                <Icon name="retry" />
              </Show>
              {retrying() ? "Retrying…" : "Retry now"}
            </button>
            <button
              type="button"
              class="remove-link"
              onClick={() => setConfirming(true)}
            >
              <Icon name="remove-feed" />
              {props.feed.connector === "reddit"
                ? "Unsubscribe"
                : "Remove feed"}
            </button>
          </Show>
        </div>
      </aside>
    </div>
  );
}

function ExtractionQuality(props: { feed: Feed }) {
  const available = () =>
    props.feed.extraction_success_rate !== undefined &&
    props.feed.average_extract_quality !== undefined;
  const label = () => {
    if (available())
      return `${Math.round((props.feed.extraction_success_rate ?? 0) * 100)}% · ${(props.feed.average_extract_quality ?? 0).toFixed(2)}`;
    if ((props.feed.extraction_sample ?? 0) > 0) return "not yet";
    return "— · —";
  };
  return (
    <div class="quality-value">
      <Show when={available()}>
        <i>
          <b
            style={{
              width: `${Math.round((props.feed.extraction_success_rate ?? 0) * 100)}%`,
            }}
          />
        </i>
      </Show>
      <span>{label()}</span>
    </div>
  );
}

function AddFeedDialog(props: {
  api: APIClient;
  allTags: string[];
  onClose(): void;
  onAdded(feed: Feed): void;
  onToast(kind: "success" | "error", message: string): void;
}) {
  const [address, setAddress] = createSignal("");
  const [state, setState] = createSignal<DiscoveryState>("idle");
  const [candidates, setCandidates] = createSignal<FeedCandidate[]>([]);
  const [selected, setSelected] = createSignal(0);
  const [tags, setTags] = createSignal<string[]>([]);
  const [error, setError] = createSignal("");
  const [errorKind, setErrorKind] = createSignal("");
  const [collection, setCollection] = createSignal<RedditCollection>("top-day");
  const [direct, setDirect] = createSignal(false);
  const [adding, setAdding] = createSignal(false);
  let timer: number | undefined;
  let input!: HTMLInputElement;
  let errorPanel: HTMLDivElement | undefined;

  onMount(() => input.focus());
  onCleanup(() => window.clearTimeout(timer));

  const resolve = async () => {
    window.clearTimeout(timer);
    const value = address().trim();
    if (!value) return;
    setState("loading");
    setError("");
    setErrorKind("");
    try {
      const result = await props.api.discoverFeed(value);
      setCandidates(result);
      setSelected(0);
      if (result[0]?.connector === "reddit")
        setCollection(redditCollectionFromURL(result[0].feed_url));
      setState(discoveredCandidateState(result));
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Discovery failed.");
      setErrorKind(caught instanceof APIError ? (caught.kind ?? "") : "");
      setState(discoveredCandidateState([], true));
      queueMicrotask(() =>
        errorPanel?.querySelector<HTMLElement>("a, button")?.focus(),
      );
    }
  };

  const scheduleResolve = () => {
    if (direct() || !address().trim()) return;
    window.clearTimeout(timer);
    timer = window.setTimeout(() => void resolve(), 400);
  };

  const edit = () => {
    setState("idle");
    input.focus();
    input.select();
  };

  const add = async () => {
    const candidate = candidates()[selected()];
    const feedURL = direct()
      ? absoluteAddress(address())
      : candidate?.connector === "reddit"
        ? redditCanonicalURL(candidate.feed_url, collection())
        : candidate?.feed_url;
    if (!feedURL) return;
    setAdding(true);
    try {
      const result = await props.api.addFeed({
        feed_url: feedURL,
        tags: tags(),
        connector: candidate?.connector,
        title: candidate?.title,
        site_url: candidate?.site_url,
        badge_url: candidate?.badge_url,
        avatar_url: candidate?.avatar_url,
      });
      props.onAdded(result.feed);
      props.onToast("success", "Feed added");
    } catch {
      props.onToast("error", "Couldn’t add feed");
    } finally {
      setAdding(false);
    }
  };

  const chosen = createMemo(() => candidates()[selected()]);

  return (
    <div class="add-feed-layer">
      <section
        class="add-feed-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="add-feed-title"
        onKeyDown={(event) => {
          if (event.key !== "Escape") return;
          event.preventDefault();
          event.stopPropagation();
          props.onClose();
        }}
      >
        <header>
          <div>
            <h2 id="add-feed-title">Add feed</h2>
            <p>A homepage, a feed URL, a channel, or r/subreddit.</p>
          </div>
          <button
            type="button"
            aria-label="Close add feed"
            onClick={props.onClose}
          >
            <Icon name="close" />
          </button>
        </header>
        <label class="add-address">
          <span>ADDRESS</span>
          <div>
            <input
              ref={input}
              value={address()}
              placeholder="example.com"
              onInput={(event) => {
                setAddress(event.currentTarget.value);
                setState("idle");
                setDirect(false);
              }}
              onBlur={scheduleResolve}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  if (state() === "single" || state() === "multiple")
                    void add();
                  else void resolve();
                }
              }}
            />
            <Show when={state() === "loading"}>
              <i
                class="field-spinner"
                role="status"
                aria-label="Finding feeds"
              />
            </Show>
          </div>
        </label>

        <Show when={state() === "single"}>
          <CandidateCard
            candidate={candidates()[0]}
            collection={collection()}
            onCollection={setCollection}
          />
        </Show>
        <Show when={state() === "multiple"}>
          <fieldset class="candidate-list">
            <legend>CHOOSE A FEED</legend>
            <For each={candidates()}>
              {(candidate, index) => (
                <label>
                  <input
                    type="radio"
                    name="candidate"
                    checked={selected() === index()}
                    onChange={() => setSelected(index())}
                  />
                  <CandidateCard candidate={candidate} compact />
                </label>
              )}
            </For>
          </fieldset>
        </Show>
        <Show when={state() === "none"}>
          <div class="discovery-note">
            <strong>No feed found</strong>
            <p>Sema checked the page head, /feed, /rss.xml, and /atom.xml.</p>
            <button
              type="button"
              onClick={() => {
                setDirect(true);
                setState("idle");
                input.focus();
              }}
            >
              Enter feed URL directly
            </button>
          </div>
        </Show>
        <Show when={state() === "error"}>
          <div
            ref={errorPanel}
            class="discovery-error"
            role="alert"
            aria-live="polite"
          >
            <strong>{error()}</strong>
            <code>{address()}</code>
            <div>
              <Show when={errorKind() === "not_found"}>
                <a
                  href={`https://www.reddit.com/search/?q=${encodeURIComponent(redditSearchTerm(address()))}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  Search Reddit for “{redditSearchTerm(address())}”
                </a>
              </Show>
              <Show when={errorKind() === "rate_limited"}>
                <button type="button" onClick={resolve}>
                  <Icon name="retry" />
                  Try again
                </button>
                <button type="button" onClick={props.onClose}>
                  Cancel
                </button>
              </Show>
              <Show
                when={
                  errorKind() !== "rate_limited" &&
                  errorKind() !== "unavailable"
                }
              >
                <button type="button" onClick={edit}>
                  Edit {errorKind() === "not_found" ? "name" : "address"}
                </button>
              </Show>
              <Show when={errorKind() === "unavailable"}>
                <button type="button" onClick={edit}>
                  Try another name
                </button>
              </Show>
            </div>
          </div>
        </Show>

        <div class="add-tags">
          <span>TAGS · OPTIONAL</span>
          <TagEditor
            tags={tags()}
            suggestions={props.allTags}
            onChange={setTags}
          />
        </div>
        <footer>
          <button type="button" onClick={props.onClose}>
            Cancel
          </button>
          <button
            type="button"
            class="primary"
            disabled={
              adding() ||
              (!direct() && state() !== "single" && state() !== "multiple") ||
              !address().trim()
            }
            onClick={add}
          >
            <Show when={!adding()}>
              <Icon name="add-feed" />
            </Show>
            {adding()
              ? "Adding…"
              : chosen()
                ? `Add ${chosen()?.title}`
                : "Add feed"}
          </button>
        </footer>
      </section>
    </div>
  );
}

function CandidateCard(props: {
  candidate: FeedCandidate;
  compact?: boolean;
  collection?: RedditCollection;
  onCollection?(collection: RedditCollection): void;
}) {
  return (
    <div
      class="candidate-card"
      classList={{
        compact: props.compact,
        reddit: props.candidate.connector === "reddit",
      }}
    >
      <SourceBadge
        connector={props.candidate.connector}
        imageURL={props.candidate.badge_url ?? props.candidate.avatar_url}
        title={props.candidate.title}
        size={36}
      />
      <span>
        <strong>{props.candidate.title}</strong>
        <small>{candidateDescriptor(props.candidate)}</small>
      </span>
      <Show
        when={
          props.candidate.connector === "reddit" &&
          props.collection &&
          props.onCollection
        }
      >
        <fieldset class="reddit-collect candidate-collect">
          <legend>COLLECT</legend>
          <RedditCollectionControl
            value={props.collection ?? "top-day"}
            onChange={(value) => props.onCollection?.(value)}
          />
          <small>{redditCollectionDescription(props.collection)}</small>
        </fieldset>
      </Show>
    </div>
  );
}

function RedditCollectionControl(props: {
  value: RedditCollection;
  onChange(collection: RedditCollection): void;
}) {
  return (
    <div class="reddit-collect-options">
      <For each={redditCollections}>
        {(collection) => (
          <button
            type="button"
            classList={{ active: props.value === collection }}
            aria-pressed={props.value === collection}
            onClick={() => props.onChange(collection)}
          >
            {redditCollectionLabel(collection)}
          </button>
        )}
      </For>
    </div>
  );
}

function MuteControl(props: {
  feed: Feed;
  working: boolean;
  onChange(muted: boolean): void;
}) {
  return (
    <label class="mute-row">
      <Icon name="mute" />
      <span>
        <strong>Mute feed</strong>
        <small>Stop fetching and hide its items.</small>
      </span>
      <input
        type="checkbox"
        checked={props.feed.muted}
        disabled={props.working}
        onChange={(event) => props.onChange(event.currentTarget.checked)}
      />
      <i />
    </label>
  );
}

function TagEditor(props: {
  tags: string[];
  suggestions: string[];
  onChange(tags: string[]): void;
}) {
  const [draft, setDraft] = createSignal("");
  const [open, setOpen] = createSignal(false);
  const matches = createMemo(() => {
    const needle = draft().trim().toLowerCase();
    if (!needle) return [];
    return props.suggestions
      .filter((tag) => !props.tags.includes(tag) && tag.includes(needle))
      .slice(0, 5);
  });
  const commit = (raw = draft()) => {
    const tag = raw.trim().toLowerCase();
    if (
      !tag ||
      props.tags.includes(tag) ||
      props.tags.length >= 10 ||
      [...tag].length > 32
    ) {
      setDraft("");
      setOpen(false);
      return;
    }
    props.onChange([...props.tags, tag].sort());
    setDraft("");
    setOpen(false);
  };
  return (
    <div class="tag-editor">
      <For each={props.tags}>
        {(tag) => (
          <span class="tag-chip">
            {tag}
            <button
              type="button"
              aria-label={`Remove ${tag} tag`}
              onClick={() =>
                props.onChange(props.tags.filter((item) => item !== tag))
              }
            >
              <Icon name="close" size={14} />
            </button>
          </span>
        )}
      </For>
      <input
        value={draft()}
        placeholder={props.tags.length ? "add" : "add a tag"}
        aria-label="Add tag"
        onFocus={() => setOpen(Boolean(draft()))}
        onInput={(event) => {
          setDraft(event.currentTarget.value);
          setOpen(true);
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === ",") {
            event.preventDefault();
            commit(matches()[0] ?? draft());
          } else if (event.key === "Escape") {
            event.preventDefault();
            event.stopPropagation();
            setDraft("");
            setOpen(false);
            event.currentTarget.blur();
          } else if (
            event.key === "Backspace" &&
            !draft() &&
            props.tags.length
          ) {
            props.onChange(props.tags.slice(0, -1));
          }
        }}
      />
      <Show when={open() && draft()}>
        <div class="tag-suggestions">
          <For each={matches()}>
            {(tag) => (
              <button
                type="button"
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => commit(tag)}
              >
                {tag}
              </button>
            )}
          </For>
          <Show
            when={!props.suggestions.includes(draft().trim().toLowerCase())}
          >
            <button
              type="button"
              class="new"
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => commit()}
            >
              new tag “{draft().trim().toLowerCase()}” <kbd>↵</kbd>
            </button>
          </Show>
        </div>
      </Show>
    </div>
  );
}

function FeedIcon(props: { feed: Feed; size?: BadgeSize }) {
  return (
    <SourceBadge
      connector={props.feed.connector}
      imageURL={props.feed.favicon_url}
      title={displayTitle(props.feed)}
      size={props.size ?? 20}
      class="feed-icon"
    />
  );
}

function DrawerStatus(props: { feed: Feed }) {
  const name = (): IconName => `status-${props.feed.status}`;
  return (
    <span class="drawer-status">
      <Icon name={name()} />
      <span>{props.feed.status}</span>
    </span>
  );
}

function StatusBadge(props: { feed: Feed }) {
  const description = () => {
    const since = brokenSince(props.feed);
    const days = since
      ? Math.floor((Date.now() - Date.parse(since)) / 86400000)
      : 0;
    const duration = since
      ? `, failing for ${days === 0 ? "less than a day" : `${days} ${days === 1 ? "day" : "days"}`}`
      : "";
    return `${props.feed.status}: ${props.feed.error_count} consecutive fetch failures${duration}`;
  };
  return (
    <Show when={props.feed.status !== "ok"}>
      <span
        role="status"
        class={`feed-status ${props.feed.status}`}
        title={description()}
        aria-label={description()}
      >
        {props.feed.status}
      </span>
    </Show>
  );
}

function PriorBadge(props: { feed: Feed }) {
  const variant = () =>
    props.feed.prior > 0.0005
      ? "positive"
      : props.feed.prior < -0.0005
        ? "negative"
        : "neutral";
  const description = () =>
    `${formatPrior(props.feed.prior)}: based on ${props.feed.prior_signals} signals in the last 90 days`;
  return (
    <span class="prior-wrap">
      <span
        role="status"
        class="prior-badge"
        classList={{ [variant()]: true }}
        title={description()}
        aria-label={description()}
      >
        {formatPrior(props.feed.prior)}
      </span>
    </span>
  );
}

function feedDescriptor(feed: Feed): string {
  if (feed.connector === "youtube") return "YouTube";
  if (feed.connector === "reddit")
    return `reddit.com · ${redditCollectionLabel(
      redditCollectionFromURL(feed.url),
    ).toLowerCase()}`;
  return domainName(feed.url);
}

function drawerDescriptor(feed: Feed): string {
  const checked = feed.last_fetch_at
    ? `checked ${relativeTime(feed.last_fetch_at)} ago`
    : "not checked yet";
  if (feed.connector === "youtube") return `YouTube · uploads · ${checked}`;
  if (feed.connector === "reddit")
    return `${feedDescriptor(feed)} · ${checked}`;
  return feed.last_fetch_at
    ? `last fetched ${relativeTime(feed.last_fetch_at)} ago`
    : "not fetched yet";
}

function candidateDescriptor(candidate: FeedCandidate): string {
  if (candidate.connector === "youtube") return "YouTube · uploads";
  if (candidate.connector === "reddit")
    return `reddit.com · ${formatCount(candidate.item_count)} items in this listing`;
  const timing = candidate.cadence
    ? ` · ${candidate.cadence}`
    : ` · ${formatCount(candidate.item_count)} items`;
  const newest = candidate.newest_item_ts
    ? ` · newest ${relativeTime(candidate.newest_item_ts)} ago`
    : "";
  return `${candidate.type.toUpperCase()}${timing}${newest}`;
}

function redditCollectionDescription(collection?: RedditCollection): string {
  switch (collection) {
    case "hot":
      return "Hot: checked every 3 hours.";
    case "new":
      return "New: checked hourly.";
    default:
      return "Top · day is the default: one pass a day, the community’s own filter, about 25 items.";
  }
}

function redditSearchTerm(raw: string): string {
  const fromURL = redditSubreddit(
    raw.includes("://") ? raw : `https://${raw.replace(/^\/?/, "")}`,
  );
  if (fromURL) return fromURL;
  return raw
    .trim()
    .replace(/^\/?r\//i, "")
    .split(/[/?#]/)[0];
}

function domainName(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    return (
      parsed.hostname.replace(/^www\./, "") +
      (parsed.pathname === "/" ? "" : parsed.pathname.replace(/\/$/, ""))
    );
  } catch {
    return rawURL;
  }
}

function absoluteAddress(raw: string): string {
  const value = raw.trim();
  return value.includes("://") ? value : `https://${value}`;
}

function formatCount(value: number): string {
  return new Intl.NumberFormat().format(value);
}

const focusSelector =
  "button, [href], input, select, textarea, [tabindex]:not([tabindex='-1'])";

const redditCollections: readonly RedditCollection[] = [
  "hot",
  "top-day",
  "new",
];
