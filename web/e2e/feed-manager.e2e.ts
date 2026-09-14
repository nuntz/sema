import { expect, type Page, test } from "@playwright/test";
import type { Feed } from "../src/types";

test.use({ timezoneId: "UTC" });

const now = new Date("2026-09-13T12:00:00Z");
const fixtures: Feed[] = Array.from({ length: 320 }, (_, index) => ({
  feed_id: `feed-${index}`,
  title:
    index < 3
      ? ["A List Apart", "Architecture Notes", "Astronomy Journal"][index]
      : `Journal ${String(index).padStart(3, "0")}`,
  url: `https://journal-${index}.example/feed`,
  connector: "rss",
  tags: [index % 2 ? "design" : "technology", `topic-${index % 8}`],
  muted: index === 4,
  hide_shorts: false,
  always_generate: index === 0,
  fetch_interval_h: index === 1 ? 1 : 24,
  last_fetch_at: index === 5 ? undefined : now.toISOString(),
  last_error: index < 3 ? "HTTP 503 Service Unavailable" : undefined,
  error_count: index < 3 ? 12 : index === 6 ? 3 : index === 3 ? 1 : 0,
  next_fetch_at: "",
  prior: 0,
  prior_signals: 0,
  status:
    index < 3 || index === 6
      ? "broken"
      : index === 3
        ? "slowed"
        : index === 4
          ? "muted"
          : "ok",
  item_count: 400,
  extraction_sample: 0,
}));

async function openManager(
  page: Page,
  options: {
    failDelete?: boolean;
    failRetry?: boolean;
    emptyCounts?: boolean;
    failCounts?: boolean;
  } = {},
) {
  let feeds = fixtures.map((feed) => ({ ...feed }));
  const state = {
    counts: [] as string[],
    retries: [] as string[],
    deletes: [] as string[],
    restored: [] as Record<string, unknown>[],
    patches: [] as Record<string, unknown>[],
    activeRetries: 0,
    maxRetries: 0,
  };
  await page.clock.setFixedTime(now);
  await page.addInitScript(() => localStorage.setItem("sema.signed-in", "1"));
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (!path.startsWith("/api/")) return route.continue();
    if (path === "/api/me")
      return route.fulfill({
        json: {
          profile: {
            email: "reader@example.com",
            created_at: now.toISOString(),
            order_pref: "interest",
            heart_count: 0,
          },
          heart_count: 0,
          signal_count: 0,
          model: { explicit_count: 0, implicit_count: 0 },
        },
      });
    if (path === "/api/feeds/counts") {
      state.counts.push(url.search);
      if (options.failCounts)
        return route.fulfill({
          status: 500,
          json: { error: "Counts unavailable" },
        });
      const empty = options.emptyCounts;
      return route.fulfill({
        json: {
          feeds: empty
            ? {}
            : Object.fromEntries(
                feeds
                  .filter(
                    (feed) =>
                      ![
                        "feed-0",
                        "feed-1",
                        "feed-2",
                        "feed-4",
                        "feed-319",
                      ].includes(feed.feed_id),
                  )
                  .map((feed) => [
                    feed.feed_id,
                    {
                      all: 400,
                      unread:
                        feed.feed_id === "feed-6"
                          ? 0
                          : Number(feed.feed_id.slice(5)) + 1,
                    },
                  ]),
              ),
        },
      });
    }
    if (path === "/api/feeds" && request.method() === "GET")
      return route.fulfill({ json: { feeds } });
    if (path.endsWith("/retry")) {
      const id = path.split("/")[3];
      state.retries.push(id);
      state.activeRetries++;
      state.maxRetries = Math.max(state.maxRetries, state.activeRetries);
      const failing = options.failRetry && id === "feed-1";
      if (!failing)
        feeds = feeds.map((feed) =>
          feed.feed_id === id
            ? { ...feed, status: "ok", error_count: 0 }
            : feed,
        );
      await route.fulfill({
        status: failing ? 500 : 202,
        json: failing
          ? { error: "Retry unavailable" }
          : feeds.find((feed) => feed.feed_id === id),
      });
      state.activeRetries--;
      return;
    }
    if (request.method() === "DELETE") {
      const id = path.split("/")[3];
      state.deletes.push(id);
      if (options.failDelete && id === "feed-1")
        return route.fulfill({
          status: 500,
          json: { error: "Removal unavailable" },
        });
      feeds = feeds.filter((feed) => feed.feed_id !== id);
      return route.fulfill({ status: 204 });
    }
    if (path === "/api/feeds" && request.method() === "POST") {
      const body = request.postDataJSON();
      state.restored.push(body);
      const original = fixtures.find((feed) => feed.url === body.feed_url);
      if (!original) throw new Error("Unknown fixture restore");
      feeds.push(original);
      return route.fulfill({ json: { feed: original } });
    }
    if (path.startsWith("/api/feeds/") && request.method() === "PATCH") {
      state.patches.push(request.postDataJSON());
      return route.fulfill({
        json: feeds.find((feed) => feed.feed_id === path.split("/")[3]),
      });
    }
    if (path === "/api/items")
      return route.fulfill({ json: { items: [], next_cursor: null } });
    return route.fulfill({ json: {} });
  });
  await page.goto("/");
  await expect(page.locator(".grid-scroll")).toBeVisible();
  await expect.poll(() => state.counts.length).toBe(1);
  await page.keyboard.press("g");
  await page.keyboard.press("s");
  await expect(page.locator(".feed-manage-row")).toHaveCount(320);
  if (!options.failCounts)
    await expect(page.locator(".feed-filters")).toContainText(
      options.emptyCounts ? "Quiet this week 319" : "Quiet this week 4",
    );
  return state;
}

for (const width of [1280, 400]) {
  test(`large feed manager at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    const state = await openManager(page);
    expect(state.counts).toEqual([""]);
    const first = page.locator(".feed-manage-row").first();
    await expect(first.locator(".feed-unread")).toHaveText("0");
    await expect(first.locator(".feed-unread")).toHaveClass(/zero/);
    const chips = page.locator(".feed-filters");
    expect(
      await chips.evaluate((el) =>
        [...el.querySelectorAll("button")].every(
          (button) =>
            button.offsetTop === el.querySelector("button")?.offsetTop,
        ),
      ),
    ).toBe(true);
    await expect(first.locator("time")).toHaveText("failing 8d");
    await expect(first.locator("time")).toHaveAttribute("title", /HTTP 503/);
    await expect(first.locator(".feed-status")).toHaveAttribute(
      "aria-label",
      "broken: 12 consecutive fetch failures, failing for 8 days",
    );
    await expect(first.locator("small")).toContainText("nothing this week");
    await expect(page.locator(".feed-triage")).toContainText(
      "3 feeds have been failing for over a week.",
    );
    const underThreshold = page
      .locator(".feed-manage-row")
      .filter({ hasText: "Journal 006" });
    await expect(underThreshold.locator(".feed-status")).toHaveText("broken");
    await expect(underThreshold.locator("time")).toHaveText("failing 6h");
    expect(
      await page
        .locator(".feeds-view")
        .evaluate((el) => el.scrollWidth <= el.clientWidth),
    ).toBe(true);
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    expect(
      await first.locator(".feed-unread").evaluate((el) => {
        const r = el.getBoundingClientRect();
        return r.left >= 16 && r.right <= innerWidth - 16;
      }),
    ).toBe(true);
    expect(
      await first.evaluate((el) => {
        const copy = el
          .querySelector(".feed-row-copy")
          ?.getBoundingClientRect();
        const time = el.querySelector("time")?.getBoundingClientRect();
        return (
          copy && time && time.top >= copy.top && time.bottom <= copy.bottom
        );
      }),
    ).toBe(true);
    const attention = page.getByRole("button", {
      name: "Needs attention 5",
      exact: true,
    });
    await attention.click();
    await expect(attention).toBeFocused();
    await expect(attention).toHaveAttribute("aria-pressed", "true");
    await expect(page.locator(".feed-manage-row")).toHaveCount(5);
    const search = page.getByRole("searchbox", { name: "Search feeds" });
    await search.fill("No matching title");
    await expect(page.locator(".feed-empty")).toContainText(
      "No feeds match this filter.",
    );
    await expect(attention).toHaveText("Needs attention 5");
    await page
      .getByRole("button", { name: "Clear filter", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "All 320", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await search.press("Escape");
    await expect(search).toHaveValue("");
    await expect(page.locator(".feed-manage-row")).toHaveCount(320);
    await attention.click();
    await attention.click();
    await expect(page.locator(".feed-manage-row")).toHaveCount(320);
    const sort = page.getByRole("combobox", { name: "Sort feeds" });
    await sort.selectOption("unread");
    await expect(page.locator(".feed-manage-row").first()).toContainText(
      "Journal 318",
    );
    await expect(
      page.locator(".feed-manage-row").last().locator(".feed-unread"),
    ).toHaveText("0");
    await sort.selectOption("quietest");
    await expect(page.locator(".feed-manage-row").first()).toContainText(
      "A List Apart",
    );
    await page
      .getByRole("button", { name: "Quiet this week 4", exact: true })
      .click();
    await expect(page.locator(".feed-manage-row")).toHaveCount(4);
    await page.locator(".feed-manager-title button").click();
    await expect(attention).toHaveAttribute("aria-pressed", "true");
    await page
      .locator(".feed-triage")
      .getByRole("button", { name: "Show", exact: true })
      .click();
    await expect(
      page.getByRole("combobox", { name: "Sort feeds" }),
    ).toHaveValue("errors");
  });
}

test("triage removal confirms, handles partial failure and restores every removed feed", async ({
  page,
}) => {
  const state = await openManager(page, { failDelete: true });
  const triage = page.locator(".feed-triage");
  await triage.getByRole("button", { name: "Remove all", exact: true }).click();
  expect(state.deletes).toEqual([]);
  await triage.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(state.deletes).toEqual([]);
  await triage.getByRole("button", { name: "Remove all", exact: true }).click();
  await triage
    .getByRole("button", { name: "Confirm removal", exact: true })
    .click();
  await expect(page.locator(".feed-undo")).toContainText("2 feeds removed");
  await expect(page.locator(".feed-manage-row")).toHaveCount(318);
  await expect(triage).toContainText(
    "1 feed has been failing for over a week.",
  );
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(page.locator(".feed-manage-row")).toHaveCount(320);
  expect(state.restored.map((body) => body.feed_url).sort()).toEqual([
    fixtures[0].url,
    fixtures[2].url,
  ]);
  expect(state.patches).toContainEqual({
    muted: false,
    always_generate: true,
    fetch_interval_h: 24,
  });
  expect(state.patches).toHaveLength(2);
});

for (const failRetry of [false, true]) {
  test(`retry settles each long-broken feed and refetches (failure: ${failRetry})`, async ({
    page,
  }) => {
    const state = await openManager(page, { failRetry });
    await page
      .locator(".feed-triage")
      .getByRole("button", { name: "Retry all", exact: true })
      .click();
    await expect
      .poll(() => state.retries)
      .toEqual(["feed-0", "feed-1", "feed-2"]);
    await expect(
      page.getByText(
        failRetry
          ? "1 feed still failing · Couldn’t retry 1"
          : "All feeds recovered",
        { exact: true },
      ),
    ).toBeVisible();
    expect(state.maxRetries).toBe(1);
    await expect(page.locator(".feed-triage")).toHaveCount(failRetry ? 1 : 0);
  });
}

test("empty loaded counts are zero without another request", async ({
  page,
}) => {
  const state = await openManager(page, { emptyCounts: true });
  await expect(
    page.locator(".feed-manage-row").first().locator(".feed-unread"),
  ).toHaveText("0");
  expect(state.counts.filter((query) => !query)).toHaveLength(1);
  expect(state.counts.filter((query) => query)).toHaveLength(0);
});

test("single-feed removal uses the same undo restoration", async ({ page }) => {
  const state = await openManager(page);
  await page.locator(".feed-manage-row").first().click();
  const drawer = page.getByRole("dialog");
  await drawer
    .getByRole("button", { name: "Remove feed", exact: true })
    .click();
  await drawer.getByRole("button", { name: "Remove", exact: true }).click();
  await expect(page.locator(".feed-undo")).toContainText("Feed removed");
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(page.locator(".feed-manage-row")).toHaveCount(320);
  expect(state.restored).toHaveLength(1);
  expect(state.restored[0]).toMatchObject({
    feed_url: fixtures[0].url,
    tags: fixtures[0].tags,
  });
  expect(state.patches).toEqual([
    { muted: false, always_generate: true, fetch_interval_h: 24 },
  ]);
});

test("switching from Today requests all-time counts and restores window counts on return", async ({
  page,
}) => {
  const state = await openManager(page);
  await page.getByRole("searchbox", { name: "Search feeds" }).press("Escape");
  await page.keyboard.press("Escape");
  await expect(page.locator(".grid-scroll")).toBeVisible();
  await page.keyboard.press("g");
  await page.keyboard.press("t");
  await expect
    .poll(() =>
      state.counts.some(
        (query) =>
          new URLSearchParams(query).get("fetched_from") ===
          "2026-09-13T00:00:00.000Z",
      ),
    )
    .toBe(true);
  const before = state.counts.length;
  await page.keyboard.press("g");
  await page.keyboard.press("s");
  await expect(
    page.locator(".feed-manage-row").first().locator(".feed-unread"),
  ).toHaveText("0");
  expect(state.counts.slice(before).filter((query) => !query)).toHaveLength(1);
  const after = state.counts.length;
  await page.getByRole("searchbox", { name: "Search feeds" }).press("Escape");
  await page.keyboard.press("Escape");
  await expect
    .poll(() =>
      state.counts
        .slice(after)
        .some(
          (query) =>
            new URLSearchParams(query).get("fetched_from") ===
            "2026-09-13T00:00:00.000Z",
        ),
    )
    .toBe(true);
});

test("failed counts stay unavailable instead of making every feed quiet", async ({
  page,
}) => {
  await openManager(page, { failCounts: true });
  await expect(page.locator(".feed-unread").first()).toBeEmpty();
  await expect(
    page.getByRole("button", { name: /Quiet this week/ }),
  ).toHaveCount(0);
  const rows = page.locator(".feed-manage-row strong");
  const titles = await rows.allTextContents();
  for (const sort of ["unread", "quietest"]) {
    await page.getByRole("combobox", { name: "Sort feeds" }).selectOption(sort);
    await expect(rows).toHaveText(titles);
  }
});
