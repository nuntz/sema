import { expect, type Page, test } from "@playwright/test";

const now = new Date("2026-09-08T02:00:00Z");
const makeItem = (id: string, hours: number, size = "M") => ({
  item_id: id,
  feed_id: "feed",
  feed_title: "Daily",
  title: `Article ${id}`,
  url: `https://example.com/${id}`,
  summary: "A story to come back to.",
  summary_source: "feed",
  published_ts: new Date(now.getTime() - (168 - hours) * 3600000).toISOString(),
  fetched_ts: now.toISOString(),
  has_body: false,
  extract_quality: 1,
  score: 0.8,
  size,
  read: false,
  hearted: false,
  signal: 0,
});
const fixtures = [
  makeItem("tonight", 3),
  makeItem("tomorrow", 19, "L"),
  makeItem("later", 46, "S"),
  makeItem("fresh", 100),
];
async function open(page: Page, theme: string, empty = false) {
  page.on("pageerror", (error) => {
    throw error;
  });
  await page.clock.install({ time: now });
  await page.addInitScript((theme) => {
    localStorage.setItem("sema.signed-in", "1");
    localStorage.setItem("sema:theme", theme);
  }, theme);
  const requests: URL[] = [];
  const read = new Set<string>();
  const kept = new Set<string>();
  await page.route("**/api/**", async (route) => {
    const url = new URL(route.request().url());
    if (!url.pathname.startsWith("/api/")) return route.continue();
    requests.push(url);
    if (route.request().method() !== "GET") {
      const id = url.pathname.split("/")[3];
      if (url.pathname.endsWith("/read")) read.add(id);
      if (url.pathname.endsWith("/heart")) kept.add(id);
      return route.fulfill({ json: { heart_count: kept.size } });
    }
    if (url.pathname === "/api/me")
      return route.fulfill({
        json: {
          profile: {
            order_pref: "interest",
            email: "reader@example.com",
            heart_count: 0,
          },
          heart_count: 0,
          model: {
            explicit_count: 0,
            liked_count: 0,
            disliked_count: 0,
            implicit_count: 0,
          },
        },
      });
    if (url.pathname === "/api/feeds")
      return route.fulfill({ json: { feeds: [] } });
    if (url.pathname === "/api/feeds/counts")
      return route.fulfill({
        json: url.searchParams.has("published_from")
          ? {
              feeds: {
                feed: {
                  all: empty ? 0 : 19,
                  unread: empty ? 0 : 19,
                  tonight: empty ? 0 : 3,
                },
              },
              within48h: empty ? 0 : 19,
              tonight: empty ? 0 : 3,
            }
          : { feeds: { feed: { all: 55, unread: 50 } } },
      });
    if (url.pathname === "/api/stories")
      return route.fulfill({ json: { stories: [] } });
    if (url.pathname === "/api/items") {
      const published = url.searchParams.has("published_from");
      const selected = published
        ? empty
          ? []
          : fixtures.slice(0, 3)
        : fixtures;
      return route.fulfill({
        json: {
          items: selected.filter(
            (item) => !read.has(item.item_id) && !kept.has(item.item_id),
          ),
          next_cursor: null,
        },
      });
    }
    if (url.pathname.startsWith("/api/items/"))
      return route.fulfill({
        json: fixtures.find(
          (item) => item.item_id === url.pathname.split("/")[3],
        ),
      });
    return route.fulfill({ json: {} });
  });
  await page.goto("/");
  await expect(page.locator('[data-item-id="tonight"]')).toBeVisible();
  return requests;
}
test.use({ timezoneId: "America/Vancouver" });
for (const theme of ["dark", "light"] as const) {
  test(`Expiring groups, server counts and keyboard order in ${theme}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const requests = await open(page, theme);
    await expect(
      page.getByRole("button", { name: "3 go tonight" }),
    ).toBeVisible();
    await expect(page.locator(".expiring-count").first()).toHaveText("19");
    const before = requests.filter(
      (url) => url.pathname === "/api/stories",
    ).length;
    await page.keyboard.press("g");
    await page.keyboard.press("e");
    await expect(page.locator(".expiring-grid")).toBeVisible();
    await expect(page.locator(".expiring-group span")).toHaveText([
      "GOES TONIGHT",
      "GOES TOMORROW",
      "LATER THIS WEEK",
    ]);
    await expect(page.locator(".expiring-group small").first()).toHaveText(
      "after 22:00",
    );
    await expect(page.locator(".expiring-strip")).toContainText(
      "19 items go in the next two days — 3 of them tonight.",
    );
    await expect(page.locator(".expiring-end")).toContainText(
      "31 more unread items",
    );
    expect(
      requests.filter((url) => url.pathname === "/api/stories"),
    ).toHaveLength(before);
    const query = requests
      .filter((url) => url.pathname === "/api/items")
      .at(-1)?.searchParams;
    expect(query?.get("ascending")).toBe("true");
    expect(query?.get("unkept")).toBe("true");
    expect(query?.has("include_read")).toBe(false);
    expect(query?.has("fetched_from")).toBe(false);
    await page.keyboard.press("j");
    await expect(page.locator('[data-item-id="tomorrow"]')).toHaveClass(
      /focused/,
    );
    await page.keyboard.press("k");
    await expect(page.locator('[data-item-id="tonight"]')).toHaveClass(
      /focused/,
    );
    await page.locator(".app-header").hover();
    await page.screenshot({
      path: `e2e/screenshots/expiring-view-${theme}.png`,
    });
  });
}
test("phone uses the short chip and rules, with a truthful empty state", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await open(page, "light");
  await page.keyboard.press("g");
  await page.keyboard.press("e");
  await expect(page.locator(".expiring-strip-phone")).toBeVisible();
  await expect(page.locator(".expiring-strip-phone")).toHaveText(
    "3 go tonight · 19 in two days",
  );
  await expect(page.locator(".expiring-group span").first()).toBeHidden();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(
    390,
  );
});
test("zero count leaves the tab available and explains keeping", async ({
  page,
}) => {
  await open(page, "dark", true);
  await expect(page.locator(".expiring-count")).toHaveCount(0);
  await page.keyboard.press("g");
  await page.keyboard.press("e");
  await expect(
    page.getByRole("heading", { name: "Nothing goes in the next two days" }),
  ).toBeVisible();
  await expect(page.locator(".expiring-empty")).toContainText(
    "anything you keep with ♥ stays for good",
  );
  await expect(page.locator(".expiring-empty")).not.toContainText("oldest");
  await page.getByRole("button", { name: "back to unread g u" }).click();
  await expect(
    page.getByRole("radio", { name: "Unread", exact: true }),
  ).toHaveAttribute("aria-checked", "true");
});

for (const width of [620, 860, 1024]) {
  test(`tonight chip fits the 56px header at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await open(page, "dark");
    await expect(page.locator(".tonight-chip")).toBeVisible();
    await expect(page.locator(".app-header")).toHaveCSS("height", "56px");
    const chip = await page.locator(".tonight-chip").boundingBox();
    const tools = await page.locator(".header-tools").boundingBox();
    expect(chip && tools && chip.x + chip.width <= tools.x).toBe(true);
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBe(width);
  });
}

test("reader walks the original deadline order after items become read", async ({
  page,
}) => {
  await open(page, "dark");
  await page.keyboard.press("g");
  await page.keyboard.press("e");
  await page.locator('[data-item-id="tonight"] .cell-main').click();
  await expect(page.locator(".reader h1")).toHaveText("Article tonight");
  await page.keyboard.press("j");
  await expect(page.locator(".reader h1")).toHaveText("Article tomorrow");
  await page.keyboard.press("k");
  await expect(page.locator(".reader h1")).toHaveText("Article tonight");
});
