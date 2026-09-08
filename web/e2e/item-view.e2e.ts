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
