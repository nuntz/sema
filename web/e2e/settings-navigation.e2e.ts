import { expect, type Page, test } from "@playwright/test";

type AppState = { itemRequests: number; readBatchRequests: number };

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
  initialCursor?: string;
  pagination?: { items: typeof items; nextCursor?: string };
  polledItems?: typeof items;
  archiveItems?: typeof items;
  readAnchor?: { item_id: string; published_ts: string };
}

async function openApp(
  page: Page,
  options: OpenAppOptions = {},
): Promise<AppState> {
  const state = { itemRequests: 0, readBatchRequests: 0 };
  await page.addInitScript(() => localStorage.setItem("sema.signed-in", "1"));
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/api/")) {
      await route.continue();
      return;
    }
    if (request.method() !== "GET") {
      if (url.pathname === "/api/items/read-batch") {
        state.readBatchRequests++;
      }
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
      const cursor = url.searchParams.get("cursor");
      const responseItems = cursor
        ? (options.pagination?.items ?? [])
        : state.itemRequests === 1
          ? (options.initialItems ?? items)
          : (options.polledItems ?? options.initialItems ?? items);
      await route.fulfill({
        json: {
          items: includeRead
            ? responseItems
            : responseItems.filter((item) => !item.read),
          next_cursor: cursor
            ? (options.pagination?.nextCursor ?? null)
            : state.itemRequests === 1
              ? (options.initialCursor ?? null)
              : null,
          ...(options.readAnchor ? { read_anchor: options.readAnchor } : {}),
        },
      });
      return;
    }
    if (url.pathname === "/api/archive") {
      await route.fulfill({
        json: { items: options.archiveItems ?? [], next_cursor: null },
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

test("desktop grid actions appear only while their cell is hovered", async ({
  page,
}) => {
  const keptItems = items.map((item, index) => ({
    ...item,
    hearted: index === 0,
  }));
  await openApp(page, { initialItems: keptItems });
  const cell = page.locator(".grid-cell").first();
  const actions = cell.locator(".cell-actions");
  const marker = cell.locator(".kept-marker");

  await expect(cell).toHaveClass(/focused/);
  await expect(marker).toHaveCount(1);
  await expect(marker).toHaveCSS("opacity", "1");
  await expect(actions).toHaveCSS("opacity", "0");
  await expect(actions).toHaveCSS("pointer-events", "none");

  await cell.hover();
  await expect(marker).toHaveCSS("opacity", "0");
  await expect(actions).toHaveCSS("opacity", "1");
  await expect(actions).toHaveCSS("pointer-events", "auto");

  await page.locator(".app-header").hover();
  await expect(cell).toHaveClass(/focused/);
  await expect(marker).toHaveCSS("opacity", "1");
  await expect(actions).toHaveCSS("opacity", "0");
  await expect(actions).toHaveCSS("pointer-events", "none");

  await cell.getByRole("button", { name: "More actions" }).focus();
  await expect(marker).toHaveCSS("opacity", "0");
  await expect(actions).toHaveCSS("opacity", "1");
  await expect(actions).toHaveCSS("pointer-events", "auto");

  await page.getByRole("radio", { name: "All", exact: true }).click();
  const allCell = page.locator(".grid-cell").first();
  await expect(allCell).toHaveClass(/all-items-cell/);
  await expect(allCell.locator(".kept-marker")).toHaveCSS("opacity", "1");
  await expect(allCell.locator(".cell-actions")).toHaveCSS("opacity", "0");
});

test("desktop-width touch profiles do not force grid actions visible", async ({
  browser,
}) => {
  const archiveItems = items.map((item) => ({ ...item, hearted: true }));
  const context = await browser.newContext({
    hasTouch: true,
    viewport: { width: 1280, height: 720 },
  });
  const page = await context.newPage();

  try {
    await openApp(page, { archiveItems });
    expect(
      await page.evaluate(() => matchMedia("(pointer: coarse)").matches),
    ).toBe(true);
    const actions = page.locator(".grid-cell").first().locator(".cell-actions");
    await expect(actions).toHaveCSS("opacity", "0");
    await expect(actions).toHaveCSS("pointer-events", "none");

    await page.getByRole("button", { name: "Archive", exact: true }).click();
    const archiveCell = page.locator(".grid-cell").first();
    const heart = archiveCell.getByRole("button", {
      name: "Remove from archive",
    });
    const more = archiveCell.getByRole("button", { name: "More actions" });
    await expect(heart).toHaveCSS("width", "44px");
    await expect(heart).toHaveCSS("height", "44px");
    await expect(more).toHaveCSS("width", "44px");
    await expect(more).toHaveCSS("height", "44px");
    await expect(heart).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
    await expect(more).toHaveCSS("background-color", "rgba(0, 0, 0, 0)");
    expect(
      await heart.evaluate(
        (element) => getComputedStyle(element, "::before").width,
      ),
    ).toBe("26px");
    expect(
      await more.evaluate(
        (element) => getComputedStyle(element, "::before").width,
      ),
    ).toBe("26px");
  } finally {
    await context.close();
  }
});

test("desktop archive actions appear only while their cell is hovered", async ({
  page,
}) => {
  const archiveItems = items.map((item) => ({ ...item, hearted: true }));
  await openApp(page, { archiveItems });
  await page.getByRole("button", { name: "Archive", exact: true }).click();

  const cell = page.locator(".grid-cell").first();
  const actions = cell.locator(".cell-actions");
  const marker = cell.locator(".kept-marker");
  await expect(cell).toHaveClass(/archive-cell/);
  await expect(marker).toHaveCount(1);
  await expect(marker).toHaveCSS("opacity", "1");
  await expect(actions).toHaveCSS("opacity", "0");
  await expect(actions).toHaveCSS("pointer-events", "none");

  await cell.hover();
  await expect(marker).toHaveCSS("opacity", "0");
  await expect(actions).toHaveCSS("opacity", "1");
  await expect(actions).toHaveCSS("pointer-events", "auto");

  await page.locator(".app-header").hover();
  await expect(marker).toHaveCSS("opacity", "1");
  await expect(actions).toHaveCSS("opacity", "0");
  await expect(actions).toHaveCSS("pointer-events", "none");
});

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
  await expect(page.getByRole("button", { name: /read & clear/ })).toHaveCount(
    0,
  );
  await expect(page.getByRole("button", { name: "Clear grid" })).toHaveCount(0);

  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect.poll(() => state.itemRequests).toBeGreaterThanOrEqual(2);
  await expect(page.locator(".new-items-pill")).toContainText("new");
  await expect(endCard).toHaveCSS("border-top-width", "0px");
});

test("new items remain visible while the unread grid is scrolled", async ({
  page,
}) => {
  const incoming = {
    ...items[0],
    item_id: "new-item",
    title: "A newly fetched item",
    url: "https://example.com/items/new",
    published_ts: "2026-08-25T19:00:00.000Z",
    fetched_ts: "2026-08-25T19:00:00.000Z",
  };
  const state = await openApp(page, {
    initialItems: items,
    polledItems: [incoming, ...items],
  });
  const scrollTop = await setMidScroll(page);

  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect.poll(() => state.itemRequests).toBeGreaterThanOrEqual(2);

  const pill = page.getByRole("button", { name: "1 new" });
  await expect(pill).toBeVisible();
  await expect
    .poll(() =>
      pill.evaluate((element) => {
        const rect = element.getBoundingClientRect();
        return rect.top >= 56 && rect.bottom <= window.innerHeight;
      }),
    )
    .toBe(true);
  await expect
    .poll(() =>
      page.locator(".grid-scroll").evaluate((element) => element.scrollTop),
    )
    .toBe(scrollTop);
});

test("pagination keeps settled visible grid rows mounted", async ({ page }) => {
  const state = await openApp(page, {
    initialItems: items.slice(0, 30),
    initialCursor: "page-2",
    pagination: { items: items.slice(30, 50) },
  });
  expect(state.itemRequests).toBe(1);

  const scroller = page.locator(".grid-scroll");
  const canvas = page.locator(".virtual-canvas");
  const initialHeight = await canvas.evaluate(
    (element) => element.getBoundingClientRect().height,
  );
  const probe = await scroller.evaluate((element) => {
    const target = Math.max(
      1,
      element.scrollHeight - element.clientHeight * 3 + 2,
    );
    const row = [...element.querySelectorAll<HTMLElement>(".grid-row")].find(
      (candidate) =>
        candidate.offsetTop + candidate.offsetHeight >= target &&
        candidate.offsetTop <= target + element.clientHeight,
    );
    const cell = row?.querySelector<HTMLElement>(".grid-cell");
    const itemID = cell?.dataset.itemId;
    if (!cell || !itemID)
      throw new Error("stable pagination probe unavailable");
    cell.setAttribute("data-mount-probe", "preserved");
    return { itemID, target };
  });
  const probeCell = page.locator(`[data-item-id="${probe.itemID}"]`);

  await scroller.evaluate((element, target) => {
    element.scrollTop = target;
  }, probe.target);

  await expect.poll(() => state.itemRequests).toBe(2);
  await expect
    .poll(() =>
      canvas.evaluate((element) => element.getBoundingClientRect().height),
    )
    .not.toBe(initialHeight);
  await expect(probeCell).toHaveAttribute("data-mount-probe", "preserved");
});

test("pill insertion returns a cleared grid to its new rows", async ({
  page,
}) => {
  const prior = {
    ...items[0],
    item_id: "stale-before-clear",
    title: "Stale before clear",
    url: "https://example.com/stale-before-clear",
  };
  const incoming = items.slice(0, 2).map((item, index) => ({
    ...item,
    item_id: `new-after-clear-${index}`,
    title: `New after clear ${index}`,
    url: `https://example.com/new-after-clear/${index}`,
    published_ts: `2026-08-25T19:0${index}:00.000Z`,
    fetched_ts: `2026-08-25T19:0${index}:00.000Z`,
    read: false,
  }));
  const state = await openApp(page, {
    initialItems: [prior],
    polledItems: [...incoming, { ...prior, read: true }],
  });
  const scroller = page.locator(".grid-scroll");
  const priorCell = page.locator(`[data-item-id="${prior.item_id}"]`);
  await expect(priorCell).toHaveCount(1);
  await priorCell.hover();
  await page.keyboard.press("m");
  await expect(priorCell).toHaveClass(/read/);
  await page.keyboard.press("Shift+G");
  await page.getByRole("button", { name: "Clear grid" }).click();
  await expect(page.locator(".grid-cell")).toHaveCount(0);

  await scroller.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        );
      }),
  );

  await scroller.evaluate((element) => {
    const canvas = element.querySelector<HTMLElement>(".virtual-canvas");
    if (!canvas) throw new Error("virtual canvas unavailable");
    canvas.style.minHeight = `${element.clientHeight + 20}px`;
    element.scrollTop = 16;
  });
  await expect
    .poll(() => scroller.evaluate((element) => element.scrollTop))
    .toBe(16);

  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect.poll(() => state.itemRequests).toBeGreaterThanOrEqual(2);
  await page.getByRole("button", { name: "2 new" }).click();

  await expect
    .poll(() => scroller.evaluate((element) => element.scrollTop))
    .toBe(0);
  await expect(page.locator(".grid-cell")).toHaveCount(2);
  await expect
    .poll(() =>
      page
        .locator(".grid-cell")
        .evaluateAll((elements) =>
          elements.map((element) => element.getAttribute("data-item-id")),
        ),
    )
    .toEqual(incoming.map((item) => item.item_id));
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
    page.getByRole("button", { name: /read & clear/ }),
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

test("finish and clear focuses the empty grid and u restores it", async ({
  page,
}) => {
  await openApp(page);
  await page.keyboard.press("Shift+G");

  const action = page.getByRole("button", { name: /Mark \d+ read & clear/ });
  const scrollBeforeClear = await page
    .locator(".grid-scroll")
    .evaluate((element) => element.scrollTop);
  await action.click();

  await expect(page.locator(".grid-cell")).toHaveCount(0);
  await expect(page.locator(".end-of-feed")).toHaveClass(/empty-grid/);
  await expect(
    page.getByRole("heading", { name: "You're all caught up" }),
  ).toBeVisible();
  await expect(page.locator(".grid-scroll")).toBeFocused();
  await expect(page.locator(".finish-undo-toast")).toContainText(
    "grid cleared",
  );

  await page.keyboard.press("u");

  await expect(page.locator(".finish-undo-toast")).toHaveCount(0);
  await expect(page.locator(".end-of-feed")).not.toHaveClass(/empty-grid/);
  await expect
    .poll(() =>
      page.locator(".grid-scroll").evaluate((element) => element.scrollTop),
    )
    .toBe(scrollBeforeClear);
  await page.keyboard.press("Shift+G");
  await expect(action).toBeFocused();
});

test("clear-only restores the all-read grid without a read batch", async ({
  page,
}) => {
  const state = await openApp(page, { initialItems: items.slice(0, 3) });
  const cells = page.locator(".grid-cell");
  await expect(cells).toHaveCount(3);
  const originalIDs = await cells.evaluateAll((elements) =>
    elements.map((element) => element.getAttribute("data-item-id") ?? ""),
  );

  for (const id of originalIDs) {
    const cell = page.locator(`[data-item-id="${id}"]`);
    await cell.hover();
    await page.keyboard.press("m");
    await expect(cell).toHaveClass(/read/);
  }
  const focusedID = originalIDs[1];
  await page.locator(`[data-item-id="${focusedID}"]`).hover();

  await page.keyboard.press("Shift+G");
  const endCard = page.locator(".end-of-feed");
  const action = page.getByRole("button", { name: "Clear grid" });
  await expect(endCard).toHaveClass(/finish-card--all-read/);
  await expect(
    page.getByRole("heading", {
      name: "All caught up — everything here is already read",
    }),
  ).toBeVisible();
  await expect(endCard).toContainText(
    "Clearing marks nothing — it just empties the grid.",
  );
  await expect(page.locator(".finish-card__mark")).toHaveCSS("width", "22px");
  await expect(action).toBeFocused();
  const scrollBeforeClear = await page
    .locator(".grid-scroll")
    .evaluate((element) => element.scrollTop);
  expect(scrollBeforeClear).toBeGreaterThan(0);

  await action.click();

  await expect(cells).toHaveCount(0);
  await expect(page.locator(".finish-undo-toast > span")).toHaveText(
    "Grid cleared",
  );
  expect(state.readBatchRequests).toBe(0);

  await page.keyboard.press("u");

  await expect(cells).toHaveCount(3);
  await expect
    .poll(() =>
      cells.evaluateAll((elements) =>
        elements.map((element) => element.getAttribute("data-item-id") ?? ""),
      ),
    )
    .toEqual(originalIDs);
  await expect(page.locator(".grid-cell.focused")).toHaveAttribute(
    "data-item-id",
    focusedID,
  );
  await expect
    .poll(() =>
      page.locator(".grid-scroll").evaluate((element) => element.scrollTop),
    )
    .toBe(scrollBeforeClear);
  expect(state.readBatchRequests).toBe(0);
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
