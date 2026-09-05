import { expect, test } from "@playwright/test";

const published = "2026-09-04T18:00:00Z";
const item = (itemID: string, feedID: string, title: string) => ({
  item_id: itemID,
  story_id: itemID === "grid" ? undefined : "story-one",
  feed_id: feedID,
  feed_title: `Feed ${feedID}`,
  connector: "rss",
  url: `https://example.com/${itemID}`,
  title,
  summary: `Summary for ${title}`,
  summary_source: "feed",
  published_ts: published,
  fetched_ts: published,
  has_body: false,
  extract_quality: 0.8,
  score: itemID === "lead" ? 1 : 0.7,
  size: itemID === "lead" ? "L" : "S",
  read: false,
  signal: 0,
  hearted: false,
  ...(itemID === "lead"
    ? { media_url: "/sema-mark.svg", media_w: 320, media_h: 180 }
    : {}),
});

test("front-page stories precede the grid and share its keyboard/read paths", async ({
  page,
}) => {
  test.setTimeout(60_000);
  const lead = item("lead", "one", "Lead coverage");
  const headline = item("headline", "two", "Another source");
  const grid = item("grid", "three", "Single-source item");
  const readBatches: string[][] = [];

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
        const body = request.postDataJSON() as { ids: string[] };
        readBatches.push(body.ids);
      }
      await route.fulfill({ status: 204 });
      return;
    }
    if (url.pathname === "/api/me") {
      await route.fulfill({
        json: {
          profile: {
            email: "reader@example.com",
            created_at: published,
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
      return;
    }
    if (url.pathname === "/api/feeds") {
      await route.fulfill({ json: { feeds: [] } });
      return;
    }
    if (url.pathname === "/api/stories") {
      await route.fulfill({
        json: {
          stories: [
            { story_id: "story-one", source_count: 2, items: [lead, headline] },
          ],
        },
      });
      return;
    }
    if (url.pathname === "/api/items") {
      await route.fulfill({ json: { items: [grid], next_cursor: null } });
      return;
    }
    await route.fulfill({ json: {} });
  });

  await page.goto("/");
  const story = page.locator('[data-story-id="story-one"]');
  const gridCell = page.locator('[data-item-id="grid"]');
  await expect(story).toBeVisible();
  await expect(gridCell).toBeVisible();
  const storyBox = await story.boundingBox();
  const gridBox = await gridCell.boundingBox();
  expect(storyBox?.y).toBeLessThan(gridBox?.y ?? 0);

  await expect(story).toHaveClass(/focused/);
  await page.keyboard.press("j");
  await expect(story.locator('[data-focus-id="headline"]')).toHaveClass(
    /focused/,
  );
  await page.keyboard.press("j");
  await expect(gridCell).toHaveClass(/focused/);
  await page.keyboard.press("Home");
  await expect(story).toHaveClass(/focused/);
  await expect(story.locator(".story-lead")).toBeFocused();

  await page.getByRole("radio", { name: "Latest", exact: true }).click();
  await expect(story).toHaveCount(0);

  await page.getByRole("radio", { name: "Front page", exact: true }).click();
  await expect(story).toBeVisible();

  for (const width of [1440, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(story).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBeLessThanOrEqual(width);
    await expect(page.locator(".app-header")).toHaveCSS("height", "56px");
  }
  await expect(story.locator(".story-media")).toHaveCSS("height", "144px");

  for (const theme of ["dark", "light"] as const) {
    await page.evaluate(
      (nextTheme) =>
        document.documentElement.setAttribute("data-theme", nextTheme),
      theme,
    );
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await page.screenshot({
        path: `/tmp/sema-front-${width}-${theme}.png`,
        fullPage: true,
      });
    }
  }

  await page.locator('[data-story-id="story-one"] .story-lead').click();
  await expect(page.locator(".reader-scroll")).toBeVisible();
  await expect.poll(() => readBatches).toContainEqual(["lead", "headline"]);
});

test("reuses story exclusion and renders a stable large-item prefix", async ({
  page,
}) => {
  const stories = Array.from({ length: 20 }, (_, storyIndex) => {
    const storyID = `story-${storyIndex}`;
    return {
      story_id: storyID,
      source_count: 6,
      items: Array.from({ length: 6 }, (_, itemIndex) => ({
        ...item(
          `${storyID}-${itemIndex}`,
          `${storyIndex}-${itemIndex}`,
          `Story ${storyIndex} coverage ${itemIndex}`,
        ),
        story_id: storyID,
        size: "M",
      })),
    };
  });
  const firstPage = [
    stories[0].items[0],
    ...Array.from({ length: 99 }, (_, index) => ({
      ...item(`large-${index}`, "single", `Large singleton ${index}`),
      story_id: undefined,
      size: "L",
    })),
  ];
  let itemRequests = 0;
  const exclusionParameters: string[] = [];

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
        json: {
          profile: {
            email: "reader@example.com",
            created_at: published,
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
      return;
    }
    if (url.pathname === "/api/feeds") {
      await route.fulfill({ json: { feeds: [] } });
      return;
    }
    if (url.pathname === "/api/stories") {
      await route.fulfill({ json: { stories } });
      return;
    }
    if (url.pathname === "/api/items") {
      itemRequests++;
      exclusionParameters.push(url.searchParams.get("exclude_stories") ?? "");
      await route.fulfill({
        json: { items: firstPage, next_cursor: "next" },
      });
      return;
    }
    await route.fulfill({ json: {} });
  });

  await page.goto("/");
  const deferredRowStyles = await page
    .locator(".story-block-row")
    .last()
    .evaluate((row) => {
      const styles = getComputedStyle(row);
      return {
        contentVisibility: styles.contentVisibility,
        intrinsicBlockSize: styles.containIntrinsicBlockSize,
      };
    });
  expect(deferredRowStyles.contentVisibility).toBe("auto");
  expect(deferredRowStyles.intrinsicBlockSize).toContain("280px");
  await expect(page.locator('[data-item-id="large-0"]')).toBeAttached();
  await expect(
    page.locator('.grid-row [data-item-id="story-0-0"]'),
  ).toHaveCount(0);
  expect(itemRequests).toBe(1);
  expect(exclusionParameters).toEqual([""]);
});
