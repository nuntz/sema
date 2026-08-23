import { createResource, createSignal, For, onCleanup, Show } from "solid-js";
import type { APIClient } from "../api/client";
import { archiveSize } from "../archive";
import { formatPrior } from "../ranking-display";
import type { Feed } from "../types";
import { relativeTime } from "./Grid";

export function Feeds(props: {
  api: APIClient;
  heartCount: number;
  onBack(): void;
  onKeys(): void;
  onSignOut(): void;
  onToast(kind: "success" | "error", message: string): void;
}) {
  const [feeds, { refetch }] = createResource(() => props.api.feeds());
  const [account, { refetch: refetchAccount, mutate: setAccount }] =
    createResource(() => props.api.me());
  const [message, setMessage] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [recomputing, setRecomputing] = createSignal(false);
  let input!: HTMLInputElement;

  const importFile = async (file?: File) => {
    if (!file) return;
    setBusy(true);
    setMessage("");
    try {
      const result = await props.api.importOPML(file);
      const queued = `${result.imported} ${result.imported === 1 ? "feed" : "feeds"} queued for ${result.imported === 1 ? "its" : "their"} first fetch.`;
      const unsupported = result.unsupported ?? [];
      const skipped = unsupported.length
        ? ` ${unsupported.length} YouTube ${unsupported.length === 1 ? "feed was" : "feeds were"} skipped. ${unsupported[0].reason}.`
        : "";
      setMessage(queued + skipped);
      await refetch();
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Import failed.");
    } finally {
      setBusy(false);
      input.value = "";
    }
  };

  const remove = async (feedID: string) => {
    await props.api.deleteFeed(feedID);
    await refetch();
  };

  const failing = () =>
    feeds()?.filter((feed) => feed.error_count > 0).length ?? 0;

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

  const model = () => account()?.model;
  return (
    <main class="feeds-view">
      <header class="feeds-header">
        <button type="button" class="wordmark" onClick={props.onBack}>
          Sema
        </button>
        <span>/ feeds</span>
        <span class="feeds-summary">
          {feeds()?.length ?? 0} feeds · {failing()} failing
        </span>
        <button type="button" class="keys-chip" onClick={props.onKeys}>
          ? keys
        </button>
        <button type="button" class="signout" onClick={props.onSignOut}>
          sign out
        </button>
      </header>
      <section class="feeds-content">
        <div class="settings-group ranking-settings">
          <h2>RANKING</h2>
          <div class="settings-row">
            <span>
              <strong>Explicit signals</strong>
              <small>Thumbs and hearts you gave</small>
            </span>
            <b>{formatCount(model()?.explicit_count ?? 0)}</b>
          </div>
          <div class="settings-row">
            <span>
              <strong>Implicit signals</strong>
              <small>Opens and skips</small>
            </span>
            <b>{formatCount(model()?.implicit_count ?? 0)}</b>
          </div>
          <div class="settings-row">
            <span>
              <strong>Model updated</strong>
              <small>Runs automatically each night</small>
            </span>
            <b>
              {model()?.computed_at
                ? modelUpdated(model()?.computed_at ?? "")
                : "not yet"}
            </b>
          </div>
          <div class="recompute-row">
            <button
              type="button"
              class="recompute-button"
              disabled={recomputing()}
              aria-busy={recomputing()}
              onClick={recompute}
            >
              <Show when={recomputing()}>
                <i aria-hidden="true" />
              </Show>
              <span>{recomputing() ? "Recomputing…" : "Recompute now"}</span>
            </button>
          </div>
        </div>
        <div class="settings-group">
          <h2>STORAGE</h2>
          <div class="settings-row">
            <span>
              <strong>Kept</strong>
              <small>Archived permanently, exempt from the 7-day expiry</small>
            </span>
            <b>
              {props.heartCount} {props.heartCount === 1 ? "item" : "items"} · ~
              {archiveSize(props.heartCount)}
            </b>
          </div>
        </div>
        <div class="import-row">
          <div>
            <strong>Import your subscriptions</strong>
            <span>RSS, Atom, and JSON Feed URLs from an OPML file</span>
          </div>
          <input
            ref={input}
            type="file"
            accept=".opml,.xml,text/xml,application/xml"
            onChange={(event) => importFile(event.currentTarget.files?.[0])}
          />
          <button
            type="button"
            class="primary"
            disabled={busy()}
            onClick={() => input.click()}
          >
            {busy() ? "Importing…" : "Import OPML"}
          </button>
        </div>
        <Show when={message()}>
          <p class="form-message" role="status">
            {message()}
          </p>
        </Show>
        <div class="feed-filters">
          <span class="selected">All {feeds()?.length ?? 0}</span>
          <span classList={{ failing: failing() > 0 }}>
            Failing {failing()}
          </span>
        </div>
        <div class="feed-table">
          <div class="feed-table-head">
            <span>SOURCE</span>
            <span>STATUS</span>
            <span>LAST FETCH</span>
            <span>PRIOR</span>
            <span />
          </div>
          <Show
            when={!feeds.loading}
            fallback={<p class="loading-line">Loading feeds…</p>}
          >
            <For
              each={feeds()}
              fallback={
                <p class="loading-line">
                  No feeds yet. Import an OPML file to begin.
                </p>
              }
            >
              {(feed) => (
                <div class="feed-row">
                  <div class="feed-source">
                    <Show
                      when={feed.favicon_url}
                      fallback={<i>{(feed.title || "F")[0]}</i>}
                    >
                      <img src={feed.favicon_url} alt="" />
                    </Show>
                    <span>
                      <strong>{feed.title || feed.url}</strong>
                      <small>{feed.url}</small>
                    </span>
                  </div>
                  <span classList={{ error: feed.error_count > 0 }}>
                    {feed.last_status || "waiting"}
                  </span>
                  <span>
                    {feed.last_fetch_at
                      ? `${relativeTime(feed.last_fetch_at)} ago`
                      : "never"}
                  </span>
                  <PriorBadge feed={feed} />
                  <button
                    type="button"
                    onClick={() => remove(feed.feed_id)}
                    aria-label={`Remove ${feed.title || feed.url}`}
                  >
                    remove
                  </button>
                </div>
              )}
            </For>
          </Show>
        </div>
      </section>
    </main>
  );
}

function PriorBadge(props: { feed: Feed }) {
  const [open, setOpen] = createSignal(false);
  const [flip, setFlip] = createSignal(false);
  let timer: number | undefined;
  let badge!: HTMLButtonElement;
  let touchToggle = false;
  let openBeforeTap = false;
  const tooltipID = `prior-${props.feed.feed_id}`;
  const show = () => {
    window.clearTimeout(timer);
    setFlip(badge.getBoundingClientRect().bottom + 42 > window.innerHeight);
    setOpen(true);
  };
  const delayedShow = () => {
    window.clearTimeout(timer);
    timer = window.setTimeout(show, 400);
  };
  const hide = () => {
    window.clearTimeout(timer);
    setOpen(false);
  };
  onCleanup(() => window.clearTimeout(timer));
  const variant = () =>
    props.feed.prior > 0.0005
      ? "positive"
      : props.feed.prior < -0.0005
        ? "negative"
        : "neutral";
  return (
    <span class="prior-wrap" classList={{ flip: flip() }}>
      <button
        ref={badge}
        type="button"
        class="prior-badge"
        classList={{ [variant()]: true }}
        aria-describedby={tooltipID}
        aria-expanded={open()}
        onMouseEnter={delayedShow}
        onMouseLeave={hide}
        onFocus={show}
        onBlur={hide}
        onPointerDown={(event) => {
          touchToggle = event.pointerType !== "mouse";
          openBeforeTap = open();
        }}
        onClick={() => {
          if (!touchToggle) return;
          if (openBeforeTap) hide();
          else show();
          touchToggle = false;
        }}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            hide();
            badge.blur();
          }
        }}
      >
        {formatPrior(props.feed.prior)}
      </button>
      <Show when={open()}>
        <span id={tooltipID} role="tooltip" class="prior-tooltip">
          Based on {props.feed.prior_signals}{" "}
          {props.feed.prior_signals === 1 ? "signal" : "signals"} in the last 90
          days
        </span>
      </Show>
    </span>
  );
}

function formatCount(value: number): string {
  return new Intl.NumberFormat().format(value);
}

function modelUpdated(value: string): string {
  const relative = relativeTime(value);
  return relative === "now" ? "just now" : `${relative} ago`;
}
