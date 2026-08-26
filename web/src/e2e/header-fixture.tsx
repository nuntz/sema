// biome-ignore-all lint/a11y/useSemanticElements: The fixture mirrors the required button-based radio controls.
import { render } from "solid-js/web";
import { AppHeader } from "../components/AppHeader";
import { Icon } from "../components/Icon";
import { createMediaQuery } from "../media-query";
import type { Item } from "../types";
import { Reader } from "../ui/Reader";
import "../styles.css";

const parameters = new URLSearchParams(window.location.search);
if (parameters.get("font") === "loaded") {
  const font = document.createElement("style");
  font.textContent = `@font-face {
    font-family: "IBM Plex Mono";
    src: local("DejaVu Sans Mono");
    font-weight: 400 600;
  }`;
  document.head.append(font);
}

const item: Item = {
  item_id: "header-fixture",
  feed_id: "fixture-feed",
  feed_title: "top scoring links",
  connector: "rss",
  url: "https://example.com/original",
  title:
    "Undersea cable repairs are taking three times longer than a decade ago",
  summary: "A compact summary used by the header acceptance fixture.",
  summary_source: "feed",
  published_ts: "2026-08-25T18:00:00Z",
  fetched_ts: "2026-08-25T18:00:00Z",
  display_date: "2026-08-25T18:00:00Z",
  body_url: "/e2e/reader-body.html",
  has_body: true,
  extract_quality: 1,
  score: 1,
  size: "L",
  read: false,
  signal: 0,
  hearted: false,
};

function GridHeaderFixture() {
  const phone = createMediaQuery("(max-width: 430px)");
  return (
    <AppHeader view="grid" onHome={() => undefined}>
      <div class="header-display-controls">
        <div class="header-segments">
          <div class="segmented" role="radiogroup" aria-label="Item order">
            <button
              type="button"
              class="segmented__item"
              role="radio"
              aria-checked="true"
            >
              <span>Front page</span>
            </button>
            <button
              type="button"
              class="segmented__item"
              role="radio"
              aria-checked="false"
            >
              <span>Latest</span>
            </button>
          </div>
          <div class="segmented" role="radiogroup" aria-label="Items shown">
            <button
              type="button"
              class="segmented__item"
              role="radio"
              aria-checked="true"
            >
              <span>Unread</span>
            </button>
            <button
              type="button"
              class="segmented__item"
              role="radio"
              aria-checked="false"
            >
              <span>All</span>
            </button>
          </div>
        </div>
        <button
          type="button"
          class="chrome-btn filter-button"
          classList={{ "is-hidden": !phone() }}
        >
          <span>Front page</span>
          <Icon name="chevron-down" />
        </button>
      </div>
      <span class="header-spacer" />
      <div class="chrome-group chrome-group--icons">
        <button type="button" class="chrome-icon" aria-label="Filter by tag">
          <Icon name="tag" />
        </button>
        <button type="button" class="chrome-icon" aria-label="Search">
          <Icon name="search" />
        </button>
        <button
          type="button"
          class="chrome-icon archive-toggle"
          aria-label="Archive"
        >
          <Icon name="archive" />
        </button>
      </div>
      <span class="chrome-divider header-tools-divider" aria-hidden="true" />
      <button
        type="button"
        class="chrome-icon settings-trigger"
        aria-label="Settings"
      >
        <Icon name="settings" />
      </button>
      <button
        type="button"
        class="chrome-btn chrome-btn--icon grid-overflow-trigger"
        classList={{ "is-hidden": !phone() }}
        aria-label="More"
      >
        <Icon name="menu" />
      </button>
    </AppHeader>
  );
}

function ReaderFixture() {
  return (
    <Reader
      item={item}
      active={false}
      archive={false}
      hearted={false}
      linkActionActive={false}
      canPrevious={true}
      canNext={true}
      onClose={() => undefined}
      onHome={() => undefined}
      onPrevious={() => undefined}
      onNext={() => undefined}
      onSignal={() => undefined}
      onHeart={() => undefined}
      onCopy={() => undefined}
      onOriginal={() => undefined}
      onRelated={() => undefined}
      onRetry={() => undefined}
      onDwell={() => undefined}
    />
  );
}

const root = document.getElementById("root");
if (!root) throw new Error("Header fixture root is missing");
render(
  () =>
    parameters.get("view") === "reader" ? (
      <ReaderFixture />
    ) : (
      <GridHeaderFixture />
    ),
  root,
);
