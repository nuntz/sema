import { expect, type Page, test } from "@playwright/test";
import { INITIAL_POLL_INTERVAL } from "../src/item-list";

const makeItem = (id: string, fetched: string, read = false) => ({
  item_id: id,
  feed_id: "daily",
  feed_title: "Daily",
  url: `https://example.com/${id}`,
  title: id,
  summary: `Summary of ${id}`,
  summary_source: "feed",
  published_ts: "2026-09-01T12:00:00Z",
  fetched_ts: fetched,
  has_body: false,
  extract_quality: 0.8,
  score: 0.5,
  size: "M",
  read,
  signal: 0,
  hearted: false,
});

async function openGrid(page: Page, empty = false, tag = false) {
  await page.clock.install({ time: new Date("2026-09-08T02:00:00Z") });
  await page.addInitScript(() => localStorage.setItem("sema.signed-in", "1"));
  const requests: URL[] = [];
  const items = [
    makeItem("Today unread", "2026-09-07T07:00:00Z"),
    makeItem("Today read", "2026-09-08T01:00:00Z", true),
    makeItem("Yesterday read", "2026-09-07T06:59:59Z", true),
    makeItem("Older unread", "2026-09-05T12:00:00Z"),
  ];
  await page.route("**/api/**", async (route) => {
    const url = new URL(route.request().url());
    if (!url.pathname.startsWith("/api/")) {
      await route.continue();
      return;
    }
    if (route.request().method() !== "GET") {
      await route.fulfill({ status: 204 });
    } else if (url.pathname === "/api/me") {
      await route.fulfill({
        json: {
          profile: {
            email: "reader@example.com",
            created_at: "2026-09-01T00:00:00Z",
            order_pref: "chrono",
            tag_pref: tag ? "design" : "",
            heart_count: 0,
          },
          heart_count: 0,
          signal_count: 0,
          model: {
            explicit_count: 0,
            liked_count: 0,
            disliked_count: 0,
            implicit_count: 0,
          },
        },
      });
    } else if (url.pathname === "/api/feeds") {
      await route.fulfill({
        json: {
          feeds: [
            {
              feed_id: "daily",
              title: "Daily",
              url: "https://example.com/feed",
              tags: ["design"],
            },
            {
              feed_id: "other",
              title: "Other",
              url: "https://example.org/feed",
              tags: ["design"],
            },
          ],
        },
      });
    } else if (url.pathname === "/api/feeds/counts") {
      requests.push(url);
      const today = url.searchParams.has("fetched_from");
      await route.fulfill({
        json: {
          feeds: empty
            ? {}
            : {
                daily: { all: today ? 10 : 1200, unread: 24 },
                other: { all: today ? 8 : 4, unread: 6 },
              },
        },
      });
    } else if (
      url.pathname === "/api/items" ||
      url.pathname === "/api/stories"
    ) {
      requests.push(url);
      const from = url.searchParams.get("fetched_from");
      const before = url.searchParams.get("fetched_before");
      const selected = (empty ? [] : items).filter(
        (item) =>
          (!item.read || url.searchParams.get("include_read") === "true") &&
          (!from || Date.parse(item.fetched_ts) >= Date.parse(from)) &&
          (!before || Date.parse(item.fetched_ts) < Date.parse(before)),
      );
      await route.fulfill({
        json:
          url.pathname === "/api/stories"
            ? { stories: [] }
            : { items: selected, next_cursor: null },
      });
    } else {
      await route.fulfill({ json: {} });
    }
  });
  await page.goto("/");
  await expect(page.locator(".grid-scroll")).toBeVisible();
  return requests;
}

test.use({ timezoneId: "America/Vancouver" });
test("Escape closes related items before clearing the tag filter", async ({
  page,
}) => {
  await openGrid(page, false, true);
  const title = page.locator(".scope-cell__title");
  await expect(title).toHaveText("#design");
  await page.locator('[data-item-id="Today unread"]').hover();
  await page.keyboard.press("r");
  const panel = page.getByRole("dialog", { name: "Similar to Today unread" });
  await expect(panel).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
  await expect(title).toHaveText("#design");
  await page.keyboard.press("Escape");
  await expect(title).toHaveText("All");
});

for (const width of [1440, 393]) {
  test(`scope counts and geometry at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const requests = await openGrid(page);
    const cell = page.locator(width < 860 ? ".scope-bar" : ".scope-cell");
    const count = page.locator(
      width < 860 ? ".filter-button .scope-count" : ".scope-cell__count",
    );
    await expect(cell).toHaveText(
      width < 860
        ? "Front pageLatestAll · Unread30"
        : "All30 unread items · 2 feeds",
    );
    const heading = await cell.boundingBox();
    const first = await page
      .locator('[data-item-id="Today unread"]')
      .boundingBox();
    if (!first || !heading)
      throw new Error("Scope cell or first card is missing");
    expect(first.y).toBeGreaterThanOrEqual(heading.y + heading.height);
    await page.keyboard.press("m");
    await expect(count).toHaveText("29");
    await page.keyboard.press("a");
    await page.keyboard.press("g");
    await page.keyboard.press("t");
    await expect(cell).toHaveText(
      width < 860 ? "Front pageLatestToday18" : "Today18 items · 2 feeds",
    );
    const request = requests.findLast(
      (url) => url.pathname === "/api/feeds/counts",
    );
    expect(request?.searchParams.get("fetched_from")).toBe(
      "2026-09-07T07:00:00.000Z",
    );
    expect(request?.searchParams.get("fetched_before")).toBe(
      "2026-09-08T07:00:00.000Z",
    );
    await page.keyboard.press("#");
    await page.getByRole("option", { name: /^design/ }).click();
    await expect(cell).toHaveText(
      width < 860
        ? "#designFront pageLatestToday18"
        : "#design18 items today · 2 feeds",
    );
    await page.keyboard.press("Escape");
    await page.keyboard.press("#");
    await page.getByRole("option", { name: /^Daily/ }).click();
    await expect(
      cell.locator(
        width < 860
          ? ".scope-bar-chip > span:not(.source-badge)"
          : ".scope-cell__title",
      ),
    ).toHaveText("Daily");
    if (width < 860) {
      await expect(count).toHaveText("10");
      await expect(
        cell.getByRole("radio", { name: "Front page" }),
      ).toBeDisabled();
      await expect(cell.locator(".scope-order-lock")).toHaveText(
        "Newest first while filtering by feed. Clear the feed to use Front page.",
      );
    } else
      await expect(cell.locator(".scope-cell__meta")).toHaveText(
        "10 items today",
      );
    await expect(cell.locator(".source-badge")).toBeVisible();
    await page.keyboard.press("g");
    await page.keyboard.press("r");
    await expect(cell).toHaveCount(0);
  });
  test(`empty scopes at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openGrid(page, true);
    await expect(page.locator(".scope-cell")).toHaveCount(0);
    await expect(
      page.getByRole("heading", { name: "You're all caught up" }),
    ).toBeVisible();
    await page.keyboard.press("g");
    await page.keyboard.press("t");
    if (width >= 860)
      await expect(page.locator(".scope-cell")).toHaveText(
        "Today0 unread items",
      );
    else
      await expect(page.locator(".filter-button")).toHaveAccessibleName(
        "Today · Unread",
      );
    await expect(
      page.getByRole("heading", { name: "Nothing unread today" }),
    ).toBeVisible();
    if (width < 620) {
      await page.getByRole("button", { name: "Show all", exact: true }).click();
      await expect(page.locator(".filter-button")).toHaveAccessibleName(
        "All · Unread",
      );
    }
    await page.keyboard.press("#");
    await page.getByRole("option", { name: /^design/ }).click();
    await expect(
      page.getByRole("heading", { name: "End of #design" }),
    ).toBeVisible();
    if (width < 620)
      await page
        .locator(".end-of-feed")
        .getByRole("button", { name: "Clear tag", exact: true })
        .click();
    else await page.keyboard.press("Escape");
    if (width < 860)
      await expect(page.locator(".scope-bar-chip")).toHaveCount(0);
    else
      await expect(page.locator(".scope-cell__title")).not.toHaveText(
        "#design",
      );
  });
}
for (const width of [1280, 768, 390])
  for (const theme of ["dark", "light"] as const) {
    test(`tag screenshot ${width} ${theme}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: theme });
      await openGrid(page, false, true);
      await page.keyboard.press("g");
      await page.keyboard.press("a");
      await page.keyboard.press("a");
      await expect(
        page.locator(
          width < 860 ? ".filter-button .scope-count" : ".scope-cell__count",
        ),
      ).toHaveText("1,204");
      const meta = page.locator(
        width < 860 ? ".filter-button .scope-count" : ".scope-cell__meta",
      );
      expect(await meta.evaluate((el) => el.scrollHeight)).toBeLessThan(25);
      await page.screenshot({
        path: `/tmp/sema-scope-cell-${width}-${theme}.png`,
      });
    });
  }

test("arrival pill aligns with the scope title", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.emulateMedia({ colorScheme: "light" });
  await openGrid(page);
  await page.route("**/api/items?*", (route) =>
    route.fulfill({
      json: {
        items: [makeItem("New arrival", "2026-09-08T02:01:00Z")],
        next_cursor: null,
      },
    }),
  );
  await page.clock.fastForward(INITIAL_POLL_INTERVAL);
  const pill = page.getByRole("button", { name: "1 new", exact: true });
  await expect(pill).toBeVisible();
  const pillRect = await pill.boundingBox();
  const titleRect = await page.locator(".scope-cell__title").boundingBox();
  if (!pillRect || !titleRect) throw new Error("Pill or scope title missing");
  expect(
    Math.abs(
      pillRect.y + pillRect.height / 2 - titleRect.y - titleRect.height / 2,
    ),
  ).toBeLessThanOrEqual(2);
  await page.screenshot({ path: "/tmp/sema-scope-pill-1280-light.png" });
});

for (const grouped of [false, true]) {
  test(`opening ${grouped ? "a story" : "articles"} then clearing leaves no stale unread count`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    await openGrid(page);
    const initial = Array.from({ length: 4 }, (_, i) =>
      makeItem(`read-sequence-${i}`, "2026-09-07T12:00:00Z"),
    );
    const incoming = makeItem("pending-arrival", "2026-09-08T02:01:00Z");
    const read = new Set<string>();
    let arrivals = false;
    await page.route("**/api/**", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (request.method() !== "GET") {
        if (url.pathname === "/api/items/read-batch") {
          const body = request.postDataJSON();
          for (const id of body.ids)
            body.read === false ? read.delete(id) : read.add(id);
        } else if (url.pathname.endsWith("/read")) {
          const id = decodeURIComponent(url.pathname.split("/")[3]);
          request.postDataJSON().read === false
            ? read.delete(id)
            : read.add(id);
        }
        return route.fulfill({ status: 204 });
      }
      if (url.pathname === "/api/feeds/counts")
        return route.fulfill({
          json: {
            feeds: {
              daily: {
                all: 4 + Number(arrivals),
                unread: 4 - read.size + Number(arrivals),
              },
            },
          },
        });
      if (url.pathname === "/api/items")
        return route.fulfill({
          json: {
            items: [
              ...(arrivals ? [incoming] : []),
              ...(grouped ? initial.slice(2) : initial),
            ].map((item) => ({ ...item, read: read.has(item.item_id) })),
            next_cursor: null,
          },
        });
      if (url.pathname === "/api/stories")
        return route.fulfill({
          json: {
            stories: grouped
              ? [
                  {
                    story_id: "read-sequence",
                    source_count: 2,
                    order_key: 0.9,
                    size: "L",
                    items: initial.slice(0, 2).map((item) => ({
                      ...item,
                      story_id: "read-sequence",
                      read: read.has(item.item_id),
                    })),
                  },
                ]
              : [],
          },
        });
      if (url.pathname.startsWith("/api/items/")) {
        const item = initial.find(
          (item) =>
            item.item_id === decodeURIComponent(url.pathname.split("/")[3]),
        );
        if (item)
          return route.fulfill({
            json: { ...item, read: read.has(item.item_id) },
          });
      }
      return route.fallback();
    });
    await page.reload();
    if (grouped)
      await page
        .getByRole("radio", { name: "Front page", exact: true })
        .click();
    const count = page.locator(".scope-cell__count");
    await expect(count).toHaveText("4");
    if (grouped) {
      await page.locator('[data-story-id="read-sequence"] .story-lead').click();
      await expect(page.locator(".reader")).toBeVisible();
      await page.keyboard.press("Escape");
    } else {
      for (const item of initial.slice(0, 2)) {
        await page
          .locator(`[data-item-id="${item.item_id}"] .cell-main`)
          .click();
        await expect(page.locator(".reader")).toBeVisible();
        await page.keyboard.press("Escape");
      }
    }
    await expect(count).toHaveText("2");
    arrivals = true;
    await page.clock.fastForward(INITIAL_POLL_INTERVAL);
    await expect(
      page.getByRole("button", { name: "1 new", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Mark 2 read & clear", exact: true })
      .click();
    await expect(page.locator(".grid-cell, .story-card")).toHaveCount(0);
    await expect(page.locator(".scope-cell")).toHaveCount(0);
    await expect(
      page.getByRole("heading", { name: "You're all caught up" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "1 new", exact: true }).click();
    await expect(
      page.locator('[data-item-id="pending-arrival"]'),
    ).toBeVisible();
    await expect(count).toHaveText("1");
  });
}

for (const [width, height, theme] of [
  [1440, 900, "light"],
  [1440, 900, "dark"],
  [390, 844, "light"],
] as const) {
  test(`closed section ${width} ${theme}`, async ({ page }) => {
    await page.setViewportSize({ width, height });
    await page.emulateMedia({ colorScheme: theme });
    await openGrid(page, true);
    await page.keyboard.press("#");
    await page.getByRole("option", { name: /^design/ }).click();
    const section = page.locator(".end-of-feed.empty-grid");
    await expect(section.getByRole("heading")).toHaveText("End of #design");
    await expect(section.getByRole("button").first()).toHaveText(
      width < 620 ? "Clear tagEsc" : "Show read itemsA",
    );
    if (width >= 860)
      await expect(page.locator(".scope-cell__count")).toHaveAttribute(
        "data-zero",
        "true",
      );
    else
      await expect(page.locator(".filter-button .scope-count")).toHaveText("0");
    const cell = await page
      .locator(width < 860 ? ".scope-bar" : ".scope-cell")
      .boundingBox();
    const rect = await section.boundingBox();
    if (!cell || !rect) throw new Error("Missing section or scope cell");
    expect(rect.y - cell.y - cell.height).toBe(width < 860 ? 16 : 26);
    const copy = await section.locator(":scope > div").boundingBox();
    expect(copy?.x).toBe(width < 620 ? 12 : 16);
    expect(
      await section.evaluate((el) => getComputedStyle(el, "::before").content),
    ).toBe("none");
    await page.screenshot({
      path: `/tmp/sema-closed-section-${width}-${theme}.png`,
      animations: "disabled",
    });
  });
}

test("same-window counts stay numeric while a new window shows counting", async ({
  page,
}) => {
  await openGrid(page);
  const count = page.locator(".scope-cell__count");
  await expect(count).toHaveText("30");
  let started!: () => void;
  const refreshing = new Promise<void>((resolve) => {
    started = resolve;
  });
  // openGrid supplied the first response; every subsequent counts response waits.
  await page.route("**/api/feeds/counts*", async (route) => {
    const today = new URL(route.request().url()).searchParams.has(
      "fetched_from",
    );
    started();
    await new Promise((resolve) => setTimeout(resolve, 400));
    await route.fulfill({
      json: {
        feeds: {
          daily: { all: today ? 10 : 1200, unread: today ? 9 : 23 },
          other: { all: today ? 8 : 6, unread: 6 },
        },
      },
    });
  });
  await page.locator('[data-item-id="Today unread"]').hover();
  await page.keyboard.press("m");
  await expect(count).toHaveText("29");
  // Opening the tag picker flushes the read batch and refreshes the same window.
  await page.keyboard.press("#");
  await refreshing;
  const samples: (string | null)[] = [];
  for (let elapsed = 0; elapsed <= 400; elapsed += 50) {
    // Read synchronously: locator.textContent() would wait through a missing count.
    samples.push(
      await page.evaluate(
        () => document.querySelector(".scope-cell__count")?.textContent ?? null,
      ),
    );
    if (elapsed < 400) await new Promise((resolve) => setTimeout(resolve, 50));
  }
  expect(samples).toHaveLength(9);
  for (const sample of samples) expect(sample ?? "").toMatch(/^\d[\d,]*$/);
  await expect(count).toHaveText("29");
  await page.keyboard.press("Escape");
  const changedWindow = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return (
      url.pathname === "/api/feeds/counts" &&
      url.searchParams.has("fetched_from")
    );
  });
  await page.keyboard.press("g");
  await page.keyboard.press("t");
  await changedWindow;
  await expect(page.locator(".scope-cell__meta")).toContainText("counting");
  await expect(count).toHaveCount(0);
  await expect(count).toHaveText("15");
});

test("filter sheet fetches missing windows in parallel and caches until refresh", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openGrid(page, false, true);
  await expect(page.locator(".filter-button .scope-count")).toHaveText("30");
  const requests: string[] = [];
  const releases: Array<() => void> = [];
  await page.route("**/api/feeds/counts?*", async (route) => {
    requests.push(route.request().url());
    await new Promise<void>((resolve) => releases.push(resolve));
    await route.fulfill({
      json: {
        feeds: { daily: { all: 10, unread: 3 }, other: { all: 8, unread: 2 } },
      },
    });
  });
  await page.locator(".filter-button").click();
  const sheet = page.getByRole("dialog", { name: "Filter", exact: true });
  await expect.poll(() => requests.length).toBe(2);
  await expect(sheet.locator(".filter-date .scope-count")).toHaveText([
    "–",
    "–",
    "30",
  ]);
  for (const release of releases) release();
  await expect(sheet.locator(".filter-date .scope-count")).toHaveText([
    "5",
    "5",
    "30",
  ]);
  await page.getByRole("button", { name: "Close filter" }).click();
  await page.locator(".filter-button").click();
  await expect(sheet.locator(".filter-date .scope-count")).toHaveText([
    "5",
    "5",
    "30",
  ]);
  expect(requests).toHaveLength(2);
  await sheet.getByRole("switch", { name: "Unread only" }).uncheck();
  await expect.poll(() => requests.length).toBe(4);
  for (const release of releases) release();
  await expect(sheet.locator(".filter-date .scope-count")).toHaveText([
    "18",
    "18",
    "1,204",
  ]);
  await page.keyboard.press("Escape");
});

for (const width of [390, 768, 1280])
  for (const theme of ["dark", "light"] as const)
    test(`Drop 25 capture ${width} ${theme}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: theme });
      await openGrid(page);
      await expect(
        page.locator(
          width < 860 ? ".filter-button .scope-count" : ".scope-cell__count",
        ),
      ).toHaveText("30");
      await page.screenshot({
        path: `/tmp/sema-drop25-${width}-${theme}.png`,
        animations: "disabled",
      });
      if (width < 860) {
        await page.locator(".filter-button").click();
        await expect(
          page.getByRole("dialog", { name: "Filter", exact: true }),
        ).toBeVisible();
        await page.screenshot({
          path: `/tmp/sema-drop25-${width}-${theme}-sheet.png`,
          animations: "disabled",
        });
      }
    });
