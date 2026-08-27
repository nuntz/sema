import { expect, type Page, test } from "@playwright/test";

type AppState = { itemRequests: number };

const profile = {
  email: "reader@example.com",
  created_at: "2026-01-01T00:00:00Z",
  order_pref: "interest",
  tag_pref: "tech",
  heart_count: 0,
};

const model = {
  explicit_count: 0,
  liked_count: 0,
  disliked_count: 0,
  implicit_count: 0,
};

const feed = {
  feed_id: "tech-feed",
  url: "https://example.com/feed.xml",
  connector: "rss",
  title: "Tech feed",
  tags: ["tech"],
  muted: false,
  hide_shorts: false,
  always_generate: false,
  fetch_interval_h: 1,
  last_fetch_at: "2026-08-25T18:00:00Z",
  error_count: 0,
  next_fetch_at: "2026-08-25T19:00:00Z",
  prior: 0,
  prior_signals: 0,
  status: "ok",
  item_count: 80,
  extraction_sample: 0,
};

const items = Array.from({ length: 80 }, (_, index) => ({
  item_id: `item-${index}`,
  feed_id: feed.feed_id,
  feed_title: feed.title,
  connector: "rss",
  url: `https://example.com/items/${index}`,
  title: `A sufficiently descriptive fixture item number ${index}`,
  summary: "Fixture summary text for keyboard navigation coverage.",
  summary_source: "feed",
  published_ts: new Date(
    Date.UTC(2026, 7, 25, 18, 0, 0) - index * 60_000,
  ).toISOString(),
  fetched_ts: new Date(
    Date.UTC(2026, 7, 25, 18, 0, 0) - index * 60_000,
  ).toISOString(),
  has_body: false,
  extract_quality: 0,
  score: 1 - index / 100,
  size: index % 7 === 0 ? "L" : index % 3 === 0 ? "M" : "S",
  read: index >= 64,
  signal: 0,
  hearted: false,
}));

interface OpenAppOptions {
  initialItems?: typeof items;
  polledItems?: typeof items;
  readAnchor?: { item_id: string; published_ts: string };
}

async function openApp(
  page: Page,
  options: OpenAppOptions = {},
): Promise<AppState> {
  const state = { itemRequests: 0 };
  await page.addInitScript(() => localStorage.setItem("sema.signed-in", "1"));
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/api/")) {
      await route.continue();
      return;
    }
    if (request.method() !== "GET") {
      await route.fulfill({ status: 204 });
      return;
    }
    if (url.pathname === "/api/me") {
      await route.fulfill({
        json: { profile, heart_count: 0, signal_count: 0, model },
      });
      return;
    }
    if (url.pathname === "/api/feeds") {
      await route.fulfill({ json: { feeds: [feed] } });
      return;
    }
    if (url.pathname === "/api/items") {
      state.itemRequests++;
      const includeRead = url.searchParams.get("include_read") === "true";
      const responseItems =
        state.itemRequests === 1
          ? (options.initialItems ?? items)
          : (options.polledItems ?? options.initialItems ?? items);
      await route.fulfill({
        json: {
          items: includeRead
            ? responseItems
            : responseItems.filter((item) => !item.read),
          next_cursor: null,
          ...(options.readAnchor ? { read_anchor: options.readAnchor } : {}),
        },
      });
      return;
    }
    await route.fulfill({ json: {} });
  });
  await page.goto("/");
  await expect(page.locator(".grid-scroll")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Clear tag filter: #tech" }),
  ).toBeVisible();
  return state;
}

test("empty unread grid omits the end divider and zero-item action", async ({
  page,
}) => {
  const state = await openApp(page, {
    initialItems: [],
    polledItems: items,
    readAnchor: {
      item_id: "caught-up-anchor",
      published_ts: "2026-08-25T17:00:00.000Z",
    },
  });
  const endCard = page.locator(".end-of-feed");

  await expect(endCard).toHaveClass(/empty-grid/);
  await expect(endCard).toHaveCSS("border-top-width", "0px");
  await expect(
    page.getByRole("button", { name: /Mark remaining/ }),
  ).toHaveCount(0);

  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect.poll(() => state.itemRequests).toBeGreaterThanOrEqual(2);
  await expect(page.locator(".pull-refresh button")).toContainText("new");
  await expect(endCard).toHaveCSS("border-top-width", "0px");
});

async function setMidScroll(page: Page): Promise<number> {
  const scroller = page.locator(".grid-scroll");
  await scroller.evaluate((element) => {
    element.scrollTop = Math.min(1_200, element.scrollHeight / 2);
  });
  const top = await scroller.evaluate((element) => element.scrollTop);
  expect(top).toBeGreaterThan(0);
  return top;
}

for (const tab of ["Unread", "All"] as const) {
  test(`g s then Escape restores the ${tab} grid without refetching`, async ({
    page,
  }) => {
    const state = await openApp(page);
    if (tab === "All") {
      await page.getByRole("radio", { name: "All" }).click();
      await expect(page.getByRole("radio", { name: "All" })).toHaveAttribute(
        "aria-checked",
        "true",
      );
      await expect.poll(() => state.itemRequests).toBe(2);
    }
    const scrollTop = await setMidScroll(page);
    const requestsBeforeSettings = state.itemRequests;

    await page.keyboard.press("g");
    await page.keyboard.press("s");
    await expect(page.locator(".feeds-view")).toBeVisible();
    await page.keyboard.press("Escape");

    await expect(page.locator(".grid-scroll")).toBeVisible();
    await expect(page.getByRole("radio", { name: tab })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    await expect(
      page.getByRole("button", { name: "Clear tag filter: #tech" }),
    ).toBeVisible();
    await expect
      .poll(() =>
        page.locator(".grid-scroll").evaluate((element) => element.scrollTop),
      )
      .toBe(scrollTop);
    expect(state.itemRequests).toBe(requestsBeforeSettings);
  });
}

test("shift+G focuses the end action and g g returns to the top", async ({
  page,
}) => {
  await openApp(page);

  await page.keyboard.press("Shift+G");
  await expect
    .poll(() =>
      page.locator(".grid-scroll").evaluate((element) => element.scrollTop),
    )
    .toBeGreaterThan(0);
  await expect(
    page.getByRole("button", { name: /Mark remaining/ }),
  ).toBeFocused();

  await page.keyboard.press("g");
  await page.keyboard.press("g");
  await expect
    .poll(() =>
      page.locator(".grid-scroll").evaluate((element) => element.scrollTop),
    )
    .toBe(0);

  await expect(
    page.locator('.settings-trigger + [role="tooltip"] kbd'),
  ).toHaveText("G S");
  await page.keyboard.press("?");
  await expect(
    page.locator(".key-row").filter({ hasText: "G S" }),
  ).toContainText("feeds & settings");
  await expect(
    page.locator(".key-row").filter({ hasText: "End / ⇧ G" }),
  ).toContainText("go to caught-up card");
});

test("Settings inputs consume Escape and suppress g s while typing", async ({
  page,
}) => {
  await openApp(page);
  await page.keyboard.press("g");
  await page.keyboard.press("s");

  const search = page.getByRole("searchbox", { name: "Search feeds" });
  await search.fill("Tech");
  await search.press("Escape");
  await expect(page.locator(".feeds-view")).toBeVisible();
  await expect(search).toHaveValue("");
  await expect(search).not.toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.locator(".grid-scroll")).toBeVisible();

  await page.keyboard.press("g");
  await page.keyboard.press("s");

  await page.getByRole("button", { name: "Add feed" }).click();
  const address = page.getByPlaceholder("example.com");
  await address.press("g");
  await address.press("s");
  await expect(address).toHaveValue("gs");
  await expect(page.locator(".feeds-view")).toBeVisible();
  await expect(page.getByRole("dialog", { name: "Add feed" })).toBeVisible();

  await address.press("Escape");
  await expect(page.getByRole("dialog", { name: "Add feed" })).toBeHidden();
  await expect(page.locator(".feeds-view")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".grid-scroll")).toBeVisible();
});

test("g s toggles Settings back to the cached grid", async ({ page }) => {
  const state = await openApp(page);
  const scrollTop = await setMidScroll(page);
  const requestsBeforeSettings = state.itemRequests;

  await page.keyboard.press("g");
  await page.keyboard.press("s");
  await expect(page.locator(".feeds-view")).toBeVisible();
  await page.keyboard.press("g");
  await page.keyboard.press("s");

  await expect(page.locator(".grid-scroll")).toBeVisible();
  await expect
    .poll(() =>
      page.locator(".grid-scroll").evaluate((element) => element.scrollTop),
    )
    .toBe(scrollTop);
  expect(state.itemRequests).toBe(requestsBeforeSettings);
});
