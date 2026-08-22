import { createResource, createSignal, For, Show } from "solid-js";
import type { APIClient } from "../api/client";
import { relativeTime } from "./Grid";

export function Feeds(props: {
  api: APIClient;
  onBack(): void;
  onSignOut(): void;
}) {
  const [feeds, { refetch }] = createResource(() => props.api.feeds());
  const [message, setMessage] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  let input!: HTMLInputElement;

  const importFile = async (file?: File) => {
    if (!file) return;
    setBusy(true);
    setMessage("");
    try {
      const result = await props.api.importOPML(file);
      setMessage(`${result.imported} feeds queued for their first fetch.`);
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
        <button type="button" class="signout" onClick={props.onSignOut}>
          sign out
        </button>
      </header>
      <section class="feeds-content">
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
