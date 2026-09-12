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
async function open(
  page: Page,
  theme: string,
  empty = false,
  scope?: "tag" | "feed",
) {
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
            tag_pref: scope === "tag" ? "tech" : "",
            feed_pref: scope === "feed" ? "feed" : "",
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
          feeds: scope
            ? [
                {
                  feed_id: "feed",
                  title: "The Verge",
                  tags: ["tech"],
                  muted: false,
                },
              ]
            : [],
        },
      });
    if (url.pathname === "/api/feeds/counts")
      return route.fulfill({
        json: url.searchParams.has("published_from")
          ? {
              feeds: {
                feed: {
                  all: empty ? 0 : 3,
                  unread: empty ? 0 : 3,
                  tonight: empty ? 0 : 1,
                  unread_total: 50,
                },
              },
              unread_total: 50,
              within48h: empty ? 0 : 3,
              tonight: empty ? 0 : 1,
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
test.use({ timezoneId: "America/Vancouver", locale: "en-GB" });
for (const theme of ["dark", "light"] as const) {
  test(`Expiring groups, server counts and keyboard order in ${theme}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const requests = await open(page, theme, false, "tag");
    await expect(
      page.getByRole("button", { name: "1 go tonight" }),
    ).toBeVisible();
    await expect(page.locator(".expiring-count").first()).toHaveText("3");
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
      "3 items in #tech go in the next two days — 1 of them tonight.",
    );
    await expect(page.locator(".expiring-end")).toContainText(
      "47 more unread items",
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
      animations: "disabled",
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
    "1 go tonight · 3 in two days",
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
  await page.keyboard.press("Escape");
  await page.clock.runFor(500);
  await expect(page.locator(".reader")).toHaveCount(0);
  const cell = page.locator('[data-item-id="tonight"]');
  await expect(cell).toBeVisible();
  await expect(cell).toHaveClass(/is-read/);
  await page.keyboard.press("g");
  await page.keyboard.press("u");
  await expect(
    page.getByRole("radio", { name: "Unread", exact: true }),
  ).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("g");
  await page.keyboard.press("e");
  await expect(cell).toHaveCount(0);
});

for (const theme of ["dark", "light"] as const) {
  test(`reader track and actual deadline disappear immediately on keep in ${theme}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await open(page, theme);
    await page.locator('[data-item-id="tomorrow"] .cell-main').click();
    await expect(page.locator(".reader-day-squares i")).toHaveCount(7);
    await expect(page.locator(".reader-day-squares .spent")).toHaveCount(6);
    await expect(page.locator(".reader-day-label")).toHaveText("day 7 of 7");
    await expect(page.locator(".reader-deadline")).toHaveText(
      "Goes tomorrow at 14:00 unless you keep it.",
    );
    await expect(page.locator(".reader .expiry-pill")).toHaveText("19h left");
    await page.screenshot({
      animations: "disabled",
      path: `e2e/screenshots/expiring-reader-${theme}.png`,
    });
    await page.clock.fastForward(14 * 3600000);
    await expect(page.locator(".reader .expiry-pill")).toHaveText("5h left");
    await expect(page.locator(".reader-day-track")).toHaveClass(/urgent/);
    await page.keyboard.press("K");
    await expect(page.locator(".reader-life, .reader-deadline")).toHaveCount(0);
  });
}
for (const width of [320, 390, 620, 860]) {
  test(`urgent reader track fits at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await open(page, "dark");
    await page.locator('[data-item-id="tonight"] .cell-main').click();
    const track = page.locator(".reader-day-track");
    await expect(track).toBeVisible();
    await expect(page.locator(".reader .expiry-pill")).toBeVisible();
    await expect(page.locator(".reader-deadline")).toHaveText(
      "Goes at 22:00 tonight — about three hours.",
    );
    await expect(page.locator(".reader .app-header")).toHaveCSS(
      "height",
      width < 620 ? "44px" : "56px",
    );
    await page.screenshot({
      animations: "disabled",
      path: `/tmp/sema-reader-track-${width}.png`,
    });
    const pill = await page.locator(".reader .expiry-pill").boundingBox();
    const slot = await page.locator(".reader-slot").boundingBox();
    expect(pill && slot && pill.x + pill.width <= slot.x + slot.width + 1).toBe(
      true,
    );
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBe(width);
  });
}

test("ordinary polls do not refresh counts within five minutes", async ({
  page,
}) => {
  const requests = await open(page, "dark");
  await expect(page.locator(".tonight-chip")).toBeVisible();
  const counts = () =>
    requests.filter((url) => url.pathname === "/api/feeds/counts");
  const before = counts().length;
  expect(
    counts().filter((url) => url.searchParams.has("published_from")),
  ).toHaveLength(1);
  for (let poll = 0; poll < 2; poll++) {
    const response = page.waitForResponse(
      (response) => new URL(response.url()).pathname === "/api/items",
    );
    await page.clock.fastForward(60_000);
    await (await response).finished();
    expect(counts()).toHaveLength(before);
  }
});

test("fresh items keep full tablet reader chrome", async ({ page }) => {
  await page.setViewportSize({ width: 860, height: 900 });
  await open(page, "dark");
  await page.locator('[data-item-id="fresh"] .cell-main').click();
  await expect(page.locator(".reader-life")).toHaveCount(0);
  await expect(page.locator(".reader-deadline")).toBeVisible();
  await expect(page.locator(".chrome-group--secondary")).toBeVisible();
  await expect(
    page.locator(".chrome-group--judge .chrome-btn__label").first(),
  ).toBeVisible();
});

for (const scope of ["tag", "feed"] as const) {
  test(`Expiring strip names the ${scope} scope`, async ({ page }) => {
    await open(page, "dark", false, scope);
    await page.keyboard.press("g");
    await page.keyboard.press("e");
    await expect(page.locator(".expiring-strip-desktop")).toContainText(
      scope === "tag" ? "3 items in #tech go" : "3 items from The Verge go",
    );
    await page.setViewportSize({ width: 390, height: 900 });
    await expect(page.locator(".expiring-strip-phone")).toHaveText(
      `1 go tonight · 3 in two days · ${scope === "tag" ? "#tech" : "The Verge"}`,
    );
  });
}
for (const pages of [3, 6]) {
  test(`Expiring follows ${pages} pages with a five-page cap`, async ({
    page,
  }) => {
    await open(page, "dark");
    const items = Array.from({ length: pages * 2 }, (_, index) =>
      makeItem(`page-${index}`, 3 + index),
    );
    let calls = 0;
    await page.route("**/api/items?**", async (route) => {
      const url = new URL(route.request().url());
      if (!url.searchParams.has("published_from")) return route.fallback();
      const index = Number(url.searchParams.get("cursor") || 0);
      calls++;
      return route.fulfill({
        json: {
          items: items.slice(index * 2, index * 2 + 2),
          next_cursor: index + 1 < pages ? String(index + 1) : null,
        },
      });
    });
    await page.route("**/api/feeds/counts?**", (route) =>
      route.fulfill({
        json: {
          feeds: {
            feed: {
              unread: items.length,
              unread_total: 50,
              all: items.length,
              tonight: 2,
            },
          },
          within48h: items.length,
          unread_total: 50,
          tonight: 2,
        },
      }),
    );
    await page.keyboard.press("g");
    await page.keyboard.press("e");
    await expect.poll(() => calls).toBe(Math.min(pages, 5));
    if (pages > 5) {
      await expect(page.locator(".expiring-showing")).toHaveText(
        "showing the first 10",
      );
      await expect(page.locator(".expiring-closing-rule")).toHaveCount(0);
      await page.locator(".grid-scroll").evaluate((element) => {
        element.scrollTop = element.scrollHeight;
      });
      await page.clock.runFor(500);
      expect(calls).toBe(5);
    } else {
      await expect(page.locator(".expiring-closing-rule")).toHaveText(
        "THAT IS ALL OF THEM",
      );
      await expect(page.locator(".expiring-showing")).toHaveCount(0);
      await expect
        .poll(() =>
          page
            .locator(".expiring-group b")
            .allTextContents()
            .then((counts) =>
              counts.reduce((sum, count) => sum + Number(count), 0),
            ),
        )
        .toBe(items.length);
    }
  });
}
