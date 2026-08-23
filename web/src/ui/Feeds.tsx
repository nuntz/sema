import {
  createMemo,
  createResource,
  createSignal,
  For,
  onCleanup,
  onMount,
  Show,
} from "solid-js";
import type { APIClient } from "../api/client";
import { archiveSize } from "../archive";
import { Icon, type IconName } from "../components/Icon";
import { formatPrior } from "../ranking-display";
import type { Feed, FeedCandidate } from "../types";
import {
  type DiscoveryState,
  discoveredCandidateState,
} from "./feed-discovery";
import { upsertFeed } from "./feed-list";
import { relativeTime } from "./Grid";

type FeedSort = "title" | "updated" | "errors" | "prior";

export function Feeds(props: {
  api: APIClient;
  heartCount: number;
  onBack(): void;
  onKeys(): void;
  onSignOut(): void;
  onFeedsChanged?(): void;
  onToast(kind: "success" | "error", message: string): void;
}) {
  const [feeds, { refetch, mutate: setFeeds }] = createResource(() =>
    props.api.feeds(),
  );
  const [account, { refetch: refetchAccount, mutate: setAccount }] =
    createResource(() => props.api.me());
  const [query, setQuery] = createSignal("");
  const [sort, setSort] = createSignal<FeedSort>("title");
  const [selectedID, setSelectedID] = createSignal("");
  const [adding, setAdding] = createSignal(false);
  const [message, setMessage] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [recomputing, setRecomputing] = createSignal(false);
  const [undoFeed, setUndoFeed] = createSignal<Feed>();
  let undoTimer: number | undefined;
  let input!: HTMLInputElement;

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
  const visibleFeeds = createMemo(() => {
    const needle = query().trim().toLowerCase();
    const matches = (feeds() ?? []).filter((feed) => {
      if (!needle) return true;
      return [displayTitle(feed), feed.url, ...(feed.tags ?? [])]
        .join(" ")
        .toLowerCase()
        .includes(needle);
    });
    return [...matches].sort((first, second) => {
      switch (sort()) {
        case "updated":
          return (second.last_fetch_at ?? "").localeCompare(
            first.last_fetch_at ?? "",
          );
        case "errors":
          return (
            statusWeight(second) - statusWeight(first) ||
            displayTitle(first).localeCompare(displayTitle(second))
          );
        case "prior":
          return (
            second.prior - first.prior ||
            displayTitle(first).localeCompare(displayTitle(second))
          );
        default:
          return displayTitle(first).localeCompare(displayTitle(second));
      }
    });
  });

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

  const remove = async (feed: Feed) => {
    await props.api.deleteFeed(feed.feed_id);
    setSelectedID("");
    setUndoFeed(feed);
    window.clearTimeout(undoTimer);
    undoTimer = window.setTimeout(() => setUndoFeed(), 8_000);
    await refresh();
  };

  const undoRemove = async () => {
    const feed = undoFeed();
    if (!feed) return;
    setUndoFeed();
    window.clearTimeout(undoTimer);
    try {
      const restored = await props.api.addFeed({
        feed_url: feed.url,
        tags: feed.tags,
        custom_title: feed.custom_title,
      });
      if (feed.muted || feed.fetch_interval_h !== 1) {
        await props.api.patchFeed(restored.feed.feed_id, {
          muted: feed.muted,
          fetch_interval_h: feed.fetch_interval_h,
        });
      }
      await refresh();
      props.onToast("success", "Feed restored");
    } catch {
      props.onToast("error", "Couldn’t restore feed");
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
    <main class="feeds-view">
      <header class="feeds-header">
        <button type="button" class="wordmark" onClick={props.onBack}>
          Sema
        </button>
        <span>/ feeds</span>
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
            {feeds()?.length ?? 0} feeds · {attention()} need attention
          </p>
        </div>
        <div class="feed-toolbar">
          <label class="feed-search">
            <Icon name="search" />
            <input
              type="search"
              value={query()}
              placeholder="Search feeds"
              aria-label="Search feeds"
              onInput={(event) => setQuery(event.currentTarget.value)}
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
              <option value="updated">Last update</option>
              <option value="errors">Errors first</option>
              <option value="prior">Prior</option>
            </select>
            <Icon name="menu" class="sort-menu-icon" />
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

        <div class="feed-manage-list" aria-busy={feeds.loading}>
          <Show
            when={!feeds.loading}
            fallback={<p class="feed-empty">Loading feeds…</p>}
          >
            <For
              each={visibleFeeds()}
              fallback={
                <div class="feed-empty">
                  <p>{query() ? "No feeds match." : "No feeds yet."}</p>
                  <button type="button" onClick={() => setAdding(true)}>
                    <Icon name="add-feed" />
                    {query() ? "Add it as a feed?" : "Add your first feed"}
                  </button>
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
                    <small>{domainName(feed.url)}</small>
                  </div>
                  <StatusBadge feed={feed} />
                  <Show when={!feed.muted}>
                    <PriorBadge feed={feed} />
                  </Show>
                  <time>
                    {feed.muted
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

      <Show when={undoFeed()}>
        <div class="feed-undo" role="status">
          <span>Feed removed</span>
          <button type="button" onClick={undoRemove}>
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
  let name!: HTMLInputElement;

  onMount(() => {
    name.focus();
    const dismiss = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopPropagation();
      props.onClose();
    };
    window.addEventListener("keydown", dismiss, true);
    onCleanup(() => window.removeEventListener("keydown", dismiss, true));
  });

  const patch = async (
    value: Partial<
      Pick<Feed, "custom_title" | "tags" | "muted" | "fetch_interval_h">
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
          <FeedIcon feed={props.feed} />
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
            <small>
              {props.feed.last_fetch_at
                ? `last fetched ${relativeTime(props.feed.last_fetch_at)} ago`
                : "not fetched yet"}
            </small>
          </div>
        </header>
        <Show when={props.feed.muted}>
          <p class="paused-copy">
            paused · {props.feed.item_count} items retained
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
                  onClick={() => void patch({ fetch_interval_h: interval })}
                >
                  {interval}h
                </button>
              )}
            </For>
          </div>
        </fieldset>
        <label class="mute-row">
          <Icon name="mute" />
          <span>
            <strong>Mute feed</strong>
            <small>Stop fetching and hide its items.</small>
          </span>
          <input
            type="checkbox"
            checked={props.feed.muted}
            disabled={working()}
            onChange={(event) =>
              void patch({ muted: event.currentTarget.checked })
            }
          />
          <i />
        </label>
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
                <strong>Remove feed?</strong>
                <p>
                  {props.feed.item_count} items from the current window will
                  disappear. Kept items stay in the archive.
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
                    Remove
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
              Remove feed
            </button>
          </Show>
        </div>
      </aside>
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
  const [direct, setDirect] = createSignal(false);
  const [adding, setAdding] = createSignal(false);
  let timer: number | undefined;
  let input!: HTMLInputElement;

  onMount(() => input.focus());
  onCleanup(() => window.clearTimeout(timer));

  const resolve = async () => {
    window.clearTimeout(timer);
    const value = address().trim();
    if (!value) return;
    setState("loading");
    setError("");
    try {
      const result = await props.api.discoverFeed(value);
      setCandidates(result);
      setSelected(0);
      setState(discoveredCandidateState(result));
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "Discovery failed.");
      setState(discoveredCandidateState([], true));
    }
  };

  const scheduleResolve = () => {
    if (direct() || !address().trim()) return;
    window.clearTimeout(timer);
    timer = window.setTimeout(() => void resolve(), 400);
  };

  const add = async () => {
    const candidate = candidates()[selected()];
    const feedURL = direct() ? absoluteAddress(address()) : candidate?.feed_url;
    if (!feedURL) return;
    setAdding(true);
    try {
      const result = await props.api.addFeed({
        feed_url: feedURL,
        tags: tags(),
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
        onKeyDown={(event) => event.key === "Escape" && props.onClose()}
      >
        <header>
          <div>
            <h2 id="add-feed-title">Add feed</h2>
            <p>Paste a homepage, feed URL, or YouTube channel.</p>
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
          <CandidateCard candidate={candidates()[0]} />
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
          <div class="discovery-error" role="alert">
            <strong>Couldn’t inspect that address.</strong>
            <code>{error()}</code>
            <div>
              <button type="button" onClick={resolve}>
                <Icon name="retry" />
                Try again
              </button>
              <button
                type="button"
                onClick={() => {
                  setState("idle");
                  input.focus();
                }}
              >
                Edit address
              </button>
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

function CandidateCard(props: { candidate: FeedCandidate; compact?: boolean }) {
  return (
    <div class="candidate-card" classList={{ compact: props.compact }}>
      <i>
        <Icon name="feed-fallback" />
      </i>
      <span>
        <strong>{props.candidate.title}</strong>
        <small>
          {props.candidate.type.toUpperCase()} ·{" "}
          {formatCount(props.candidate.item_count)} items
          <Show when={props.candidate.newest_item_ts}>
            {` · newest ${relativeTime(props.candidate.newest_item_ts ?? "")} ago`}
          </Show>
        </small>
      </span>
    </div>
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
            setDraft("");
            setOpen(false);
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

function FeedIcon(props: { feed: Feed }) {
  return (
    <Show
      when={props.feed.favicon_url}
      fallback={
        <i class="feed-icon fallback">
          <Icon name="feed-fallback" />
        </i>
      }
    >
      <img class="feed-icon" src={props.feed.favicon_url} alt="" />
    </Show>
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
  const description = () =>
    `${props.feed.status}: ${props.feed.error_count} consecutive fetch failures`;
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

function displayTitle(feed: Feed): string {
  return feed.custom_title || feed.title || domainName(feed.url) || feed.url;
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

function statusWeight(feed: Feed): number {
  return feed.status === "broken"
    ? 3
    : feed.status === "slowed"
      ? 2
      : feed.status === "muted"
        ? 1
        : 0;
}

function formatCount(value: number): string {
  return new Intl.NumberFormat().format(value);
}

const focusSelector =
  "button, [href], input, select, textarea, [tabindex]:not([tabindex='-1'])";
