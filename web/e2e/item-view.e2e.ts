import { expect, type Page, test } from "@playwright/test";

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

async function openGrid(page: Page) {
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
            order_pref: "interest",
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
      await route.fulfill({ json: { feeds: [] } });
    } else if (
      url.pathname === "/api/items" ||
      url.pathname === "/api/stories"
    ) {
      requests.push(url);
      const from = url.searchParams.get("fetched_from");
      const before = url.searchParams.get("fetched_before");
      const selected = items.filter(
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
  await expect(page.locator('[data-item-id="Today unread"]')).toBeVisible();
  return requests;
}

test.use({ timezoneId: "America/Vancouver" });

for (const width of [1440, 860, 620, 393, 320]) {
  test(`calendar views include read items and fit at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const requests = await openGrid(page);
    if (width <= 859) await page.locator(".filter-button").click();
    const group =
      width <= 859
        ? page
            .getByRole("dialog", { name: "Feed view" })
            .getByRole("radiogroup", { name: "Items shown" })
        : page.getByRole("radiogroup", { name: "Items shown" });
    await expect(group.getByRole("radio")).toHaveText([
      "Unread",
      "Today",
      "Yesterday",
      "All",
    ]);
    await group.getByRole("radio", { name: "Today", exact: true }).click();
    await expect(
      group.getByRole("radio", { name: "Today", exact: true }),
    ).toBeChecked();
    await expect(page.locator('[data-item-id="Today read"]')).toBeVisible();
    await expect(page.locator('[data-item-id="Yesterday read"]')).toHaveCount(
      0,
    );
    await expect(page.locator('[data-item-id="Older unread"]')).toHaveCount(0);
    for (const endpoint of ["/api/items", "/api/stories"]) {
      const request = requests.findLast((url) => url.pathname === endpoint);
      expect(request?.searchParams.get("fetched_from")).toBe(
        "2026-09-07T07:00:00.000Z",
      );
      expect(request?.searchParams.get("fetched_before")).toBe(
        "2026-09-08T07:00:00.000Z",
      );
      expect(request?.searchParams.get("include_read")).toBe("true");
    }
    await group.getByRole("radio", { name: "Yesterday", exact: true }).click();
    await expect(page.locator('[data-item-id="Yesterday read"]')).toBeVisible();
    await expect(page.locator('[data-item-id="Today unread"]')).toHaveCount(0);
    await group.getByRole("radio", { name: "All", exact: true }).click();
    await expect(page.locator('[data-item-id="Older unread"]')).toBeVisible();
    await expect(page.locator('[data-item-id="Today read"]')).toBeVisible();
    expect(requests.at(-1)?.searchParams.has("fetched_from")).toBe(false);
    expect(requests.at(-1)?.searchParams.has("fetched_before")).toBe(false);
    const bounds = await group.boundingBox();
    if (!bounds) throw new Error("Items shown control is missing");
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBe(width);
    await group.getByRole("radio", { name: "Today", exact: true }).click();
    await expect(page.locator('[data-item-id="Today read"]')).toBeVisible();
    if (width === 1440 || width === 393) {
      await page.screenshot({ path: `/tmp/sema-item-view-${width}.png` });
    }
  });
}

test("calendar views advance at local midnight on the next poll", async ({
  page,
}) => {
  const requests = await openGrid(page);
  await page.getByRole("radio", { name: "Today", exact: true }).click();
  await expect(page.locator('[data-item-id="Today read"]')).toBeVisible();
  await page.clock.setSystemTime(new Date("2026-09-08T07:00:01Z"));
  await page.clock.fastForward(60_000);
  await expect
    .poll(() => requests.at(-1)?.searchParams.get("fetched_from"))
    .toBe("2026-09-08T07:00:00.000Z");
  await expect(page.locator('[data-item-id="Today read"]')).toHaveCount(0);
});

test("go sequences select scopes directly and preserve legacy bindings", async ({
  page,
}) => {
  const requests = await openGrid(page);
  for (const [key, label] of [
    ["t", "Today"],
    ["y", "Yesterday"],
    ["a", "All"],
    ["u", "Unread"],
  ]) {
    await page.keyboard.press("g");
    await page.keyboard.press(key);
    await expect(
      page.getByRole("radio", { name: label, exact: true }),
    ).toBeChecked();
    await expect(
      page.getByRole("radio", { name: "Front page", exact: true }),
    ).toBeChecked();
    await page.keyboard.press("g");
    await page.keyboard.press("r");
    await expect(
      page.getByRole("button", { name: "Archive", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await expect(
      page.getByRole("heading", { name: "Nothing kept yet" }),
    ).toBeVisible();
    await page.keyboard.press("g");
    await page.keyboard.press("r");
    await expect(
      page.getByRole("button", { name: "Archive", exact: true }),
    ).toHaveAttribute("aria-pressed", "true");
    await page.keyboard.press("g");
    await page.keyboard.press(key);
    await expect(
      page.getByRole("radio", { name: label, exact: true }),
    ).toBeChecked();
    await expect(
      page.getByRole("button", { name: "Archive", exact: true }),
    ).toHaveAttribute("aria-pressed", "false");
  }
  expect(
    requests
      .filter((url) => url.pathname === "/api/items")
      .every((url) => url.searchParams.get("order") === "interest"),
  ).toBe(true);
  await expect(page.locator(".loading-screen")).toHaveCount(0);
  await page.keyboard.press("a");
  await expect(
    page.getByRole("radio", { name: "All", exact: true }),
  ).toBeChecked();
  await expect(page.locator(".loading-screen")).toHaveCount(0);
  await page.keyboard.press("a");
  await expect(
    page.getByRole("radio", { name: "Unread", exact: true }),
  ).toBeChecked();
  await expect(page.locator(".loading-screen")).toHaveCount(0);
  await page.keyboard.press("Shift+A");
  await expect(
    page.getByRole("button", { name: "Archive", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(page.locator(".loading-screen")).toHaveCount(0);
  await page.keyboard.press("Shift+A");
  await expect(
    page.getByRole("radio", { name: "Unread", exact: true }),
  ).toBeChecked();
  await expect(page.locator(".grid-scroll")).toBeVisible();
  await page.keyboard.press("t");
  await expect(
    page.getByRole("radio", { name: "Latest", exact: true }),
  ).toBeChecked();
});

test("go prefixes expire and pause in inputs and dialogs", async ({ page }) => {
  await openGrid(page);
  await page.keyboard.press("g");
  await page.clock.fastForward(650);
  await page.keyboard.press("t");
  await expect(
    page.getByRole("radio", { name: "Latest", exact: true }),
  ).toBeChecked();
  await expect(
    page.getByRole("radio", { name: "Unread", exact: true }),
  ).toBeChecked();
  await page.keyboard.press("g");
  await page.keyboard.press("s");
  await expect(page.locator(".feeds-view")).toBeVisible();
  const input = page.getByRole("searchbox", { name: "Search feeds" });
  await input.focus();
  await page.keyboard.type("gtgagygugr");
  await expect(input).toHaveValue("gtgagygugr");
  await expect(page.locator(".feeds-view")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.keyboard.press("g");
  await page.keyboard.press("t");
  await expect(
    page.getByRole("radio", { name: "Today", exact: true }),
  ).toBeChecked();
  await page.keyboard.press("?");
  await expect(
    page.getByRole("dialog", { name: "Keyboard", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("g");
  await page.keyboard.press("y");
  await expect(
    page.getByRole("radio", { name: "Today", exact: true }),
  ).toBeChecked();
  await page.keyboard.press("Escape");
  await page.locator('[data-item-id="Today unread"] .cell-main').click();
  await expect(page.locator(".reader")).toBeVisible();
  await page.keyboard.press("g");
  await page.keyboard.press("t");
  await expect(page.locator(".reader")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("radio", { name: "Today", exact: true }),
  ).toBeChecked();
});

test("disabling character shortcuts persists and keeps native navigation available", async ({
  page,
}) => {
  await openGrid(page);
  await page.keyboard.press("g");
  await page.keyboard.press("s");
  await page
    .getByRole("checkbox", { name: "Letter and symbol shortcuts" })
    .uncheck();
  await page.locator(".feed-manager-title h1").click();
  await page.keyboard.press("Escape");
  await expect(page.locator(".grid-scroll")).toBeVisible();
  for (const key of ["g", "t", "a", "Shift+A", "?", "/", "#", "m", "f"])
    await page.keyboard.press(key);
  await expect(
    page.getByRole("radio", { name: "Unread", exact: true }),
  ).toBeChecked();
  await expect(
    page.getByRole("radio", { name: "Front page", exact: true }),
  ).toBeChecked();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("Enter");
  await expect(page.locator(".reader")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.reload();
  await expect(page.locator(".grid-scroll")).toBeVisible();
  await page.keyboard.press("a");
  await expect(
    page.getByRole("radio", { name: "Unread", exact: true }),
  ).toBeChecked();
  await page
    .getByRole("button", { name: "Feeds & settings", exact: true })
    .click();
  await expect(
    page.getByRole("checkbox", { name: "Letter and symbol shortcuts" }),
  ).not.toBeChecked();
  await page
    .getByRole("button", { name: "View keyboard shortcuts", exact: true })
    .click();
  const help = page.getByRole("dialog", { name: "Keyboard", exact: true });
  await expect(help.getByRole("heading")).toHaveText([
    "Keyboard",
    "Lightbox",
    "Navigation",
    "Views",
    "Item actions",
    "General",
  ]);
  await expect(help.getByText("Keep / unkeep", { exact: true })).toBeVisible();
  await expect(
    help.getByText("Show related coverage", { exact: true }),
  ).toBeVisible();
  await page.screenshot({ path: "/tmp/sema-keyboard-help-desktop.png" });
  await page.setViewportSize({ width: 393, height: 852 });
  await page.screenshot({ path: "/tmp/sema-keyboard-help-phone.png" });
  await page.setViewportSize({ width: 1280, height: 900 });
  await help
    .getByRole("checkbox", { name: "Letter and symbol shortcuts" })
    .check();
  await page.keyboard.press("Escape");
  await expect(help).toHaveCount(0);
  await page.locator(".feed-manager-title h1").click();
  await page.keyboard.press("g");
  await page.keyboard.press("y");
  await expect(
    page.getByRole("radio", { name: "Yesterday", exact: true }),
  ).toBeChecked();
});

test("archive preserves tag filtering and allows clearing and changing scopes", async ({
  page,
}) => {
  await openGrid(page);
  await page.route("**/api/feeds", (route) =>
    route.fulfill({
      json: {
        feeds: [
          {
            feed_id: "daily",
            title: "Daily",
            url: "https://example.com/feed",
            tags: ["tech", "empty"],
          },
        ],
      },
    }),
  );
  const requests: URL[] = [];
  await page.route("**/api/archive?*", (route) => {
    const url = new URL(route.request().url());
    requests.push(url);
    const tag = url.searchParams.get("tag");
    const items =
      tag === "empty"
        ? []
        : [makeItem("Kept tech", "2026-09-01T00:00:00Z", true)];
    if (!tag) items.push(makeItem("Kept other", "2026-09-01T00:00:00Z", true));
    return route.fulfill({ json: { items, next_cursor: "" } });
  });
  await page.reload();
  await page.getByRole("radio", { name: "All", exact: true }).click();
  await page.keyboard.press("#");
  await page.getByRole("option", { name: /^tech/ }).click();
  await page.getByRole("button", { name: "Archive", exact: true }).click();
  await expect(page.locator('[data-item-id="Kept tech"]')).toBeVisible();
  expect(requests.at(-1)?.searchParams.get("tag")).toBe("tech");
  await expect(page.locator('[data-item-id="Kept other"]')).toHaveCount(0);
  await page
    .getByRole("button", { name: "Clear tag filter: #tech", exact: true })
    .click();
  await expect(page.locator('[data-item-id="Kept other"]')).toBeVisible();
  expect(requests.at(-1)?.searchParams.has("tag")).toBe(false);
  await page.keyboard.press("#");
  await page.getByRole("option", { name: /^empty/ }).click();
  await expect(
    page.getByText("No archived items match this filter."),
  ).toBeVisible();
  await page.getByRole("button", { name: "Show all feeds" }).click();
  await expect(page.locator('[data-item-id="Kept other"]')).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Archive", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await page.screenshot({ path: "/tmp/sema-archive-filter-cleared.png" });
});
