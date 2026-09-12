import { expect, type Page, test } from "@playwright/test";

const now = new Date("2026-09-08T02:00:00Z");
const makeItem = (id: string, hours: number, size = "M") => ({
  item_id: id,
  feed_id: "feed",
  feed_title: "Daily reports from a very long publication name",
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
async function open(page: Page, theme: string) {
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
      if (url.pathname.endsWith("/read-batch")) {
        for (const itemID of route.request().postDataJSON().ids)
          read.add(itemID);
      }
      if (url.pathname.endsWith("/heart")) kept.add(id);
      return route.fulfill({ json: { heart_count: kept.size } });
    }
    if (url.pathname === "/api/me")
      return route.fulfill({
        json: {
          profile: {
            order_pref: "interest",
            tag_pref: "",
            feed_pref: "",
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
      return route.fulfill({
        json: {
          feeds: [],
        },
      });
    if (url.pathname === "/api/feeds/counts")
      return route.fulfill({
        json: { feeds: { feed: { all: 4, unread: 4 } } },
      });
    if (url.pathname === "/api/stories")
      return route.fulfill({ json: { stories: [] } });
    if (url.pathname === "/api/items") {
      return route.fulfill({
        json: {
          items: fixtures.filter(
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
test.use({ timezoneId: "America/Vancouver", locale: "en-GB" });
for (const theme of ["dark", "light"] as const) {
  test(`reader pill and actual deadline disappear immediately on keep in ${theme}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page, theme);
    await page.locator('[data-item-id="tomorrow"] .cell-main').click();
    await expect(page.locator('.reader-life [role="img"]')).toHaveCount(1);
    await expect(page.locator(".reader-life")).toHaveText("19h left");
    await expect(page.locator(".reader .reader-crumb__meta")).toHaveCount(0);
    await expect(page.locator(".reader-deadline")).toHaveText(
      "Goes tomorrow at 14:00 unless you keep it.",
    );
    await expect(page.locator(".reader .expiry-pill")).toHaveText("19h left");
    await expect(page.locator(".reader .expiry-pill")).toHaveRole("img");
    await expect(page.locator(".reader .expiry-pill")).toHaveAccessibleName(
      "19 hours left",
    );
    await page.clock.fastForward(14 * 3600000);
    await expect(page.locator(".reader .expiry-pill")).toHaveText("5h left");
    await page.clock.fastForward(6 * 3600000);
    await expect(page.locator(".reader .expiry-pill")).toHaveText("goes now");
    await expect(page.locator(".reader-deadline")).toHaveText(
      "Goes now unless you keep it.",
    );
    await page.keyboard.press("K");
    await expect(page.locator(".reader-life, .reader-deadline")).toHaveCount(0);
    await expect(page.locator(".reader .reader-crumb__meta")).toContainText(
      "ago",
    );
  });
}
for (const width of [320, 390, 620, 860, 1440]) {
  test(`urgent reader pill fits at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await open(page, "dark");
    await page.locator('[data-item-id="tonight"] .cell-main').click();
    await expect(page.locator(".reader .expiry-pill")).toBeVisible();
    await expect(page.locator(".reader-deadline")).toHaveText(
      "Goes at 22:00 tonight — about three hours.",
    );
    await expect(page.locator(".reader .app-header")).toHaveCSS(
      "height",
      width < 620 ? "44px" : "56px",
    );
    await expect
      .poll(() =>
        page.locator(".reader-slot").evaluate((slot) => {
          const pill = slot.querySelector(".expiry-pill");
          const identity = slot.querySelector(".reader-crumb__identity");
          if (!pill || !identity) return false;
          const pillRect = pill.getBoundingClientRect();
          const identityRect = identity.getBoundingClientRect();
          return (
            pillRect.right <= slot.getBoundingClientRect().right + 1 &&
            pillRect.left >= identityRect.right &&
            identityRect.width > 0 &&
            Math.abs(
              pillRect.top +
                pillRect.height / 2 -
                (identityRect.top + identityRect.height / 2),
            ) <= 1
          );
        }),
      )
      .toBe(true);
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBe(width);
  });
}

for (const width of [619, 620, 860, 1199, 1200]) {
  test(`fresh reader keeps full chrome at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await open(page, "dark");
    await page.locator('[data-item-id="fresh"] .cell-main').click();
    await expect(page.locator(".reader-life")).toHaveCount(0);
    await expect(page.locator(".reader-deadline")).toBeVisible();
    if (width >= 860) {
      await expect(page.locator(".chrome-group--secondary")).toBeVisible();
      await expect(
        page.locator(".chrome-group--judge .chrome-btn__label").first(),
      ).toBeVisible();
    }
  });
}
