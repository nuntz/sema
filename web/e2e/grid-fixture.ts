import { expect, type Page } from "@playwright/test";

/** The full app over one mocked Item, focused on its cell. */
export async function openGrid(
  page: Page,
  kind = "image",
  tag = false,
  size = "M",
  options: {
    story?: boolean;
    /** A second Item: the same kind, or an article that is not a Video Item. */
    second?: boolean | "article";
    read?: boolean;
    external?: boolean;
    order?: string;
  } = {},
) {
  await page.addInitScript(() => localStorage.setItem("sema.signed-in", "1"));
  const item = {
    item_id: "peek",
    feed_id: "daily",
    feed_title: "Daily",
    url:
      kind === "youtube"
        ? "https://youtu.be/dQw4w9WgXcQ"
        : "https://example.com/peek",
    title: "Image article",
    summary: "An article with images",
    published_ts: new Date().toISOString(),
    fetched_ts: new Date().toISOString(),
    has_body: kind !== "video",
    body_url: "/archive/peek.html",
    media_url: kind === "body" ? undefined : "/media/e2e/lightbox-images/1.svg",
    media_type: kind === "video" ? "video" : "image",
    media_w: 1600,
    media_h: 1000,
    extract_quality: 0.8,
    score: 0.5,
    size,
    read: options.read ?? false,
    connector: options.external ? "reddit" : "rss",
    external_url: options.external ? "https://v.redd.it/example" : undefined,
    signal: 0,
    hearted: false,
  };
  const mutations: string[] = [];
  await page.route("**/api/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (!path.startsWith("/api/")) {
      await route.continue();
      return;
    }
    if (route.request().method() !== "GET") {
      mutations.push(path);
      await route.fulfill({ status: 204 });
    } else if (path === "/api/me") {
      await route.fulfill({
        json: {
          profile: {
            email: "reader@example.com",
            order_pref: options.order ?? "interest",
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
    } else if (path === "/api/items" || path === "/api/archive")
      await route.fulfill({
        json: {
          items: options.second
            ? [
                item,
                {
                  ...item,
                  item_id: "other",
                  title: "Other image",
                  media_url: "/media/e2e/lightbox-images/2.svg",
                  ...(options.second === "article"
                    ? {
                        url: "https://example.com/other",
                        title: "Other article",
                      }
                    : {}),
                },
              ]
            : [item],
          next_cursor: null,
        },
      });
    else if (path === "/api/stories")
      await route.fulfill({
        json: {
          stories: options.story
            ? [
                {
                  story_id: "peek-story",
                  source_count: 2,
                  order_key: 1,
                  size,
                  items: [
                    { ...item, story_id: "peek-story" },
                    {
                      ...item,
                      item_id: "headline",
                      feed_id: "other",
                      title: "Another headline",
                      media_url: "/media/e2e/lightbox-images/2.svg",
                      story_id: "peek-story",
                    },
                  ],
                },
              ]
            : [],
        },
      });
    else if (path === "/api/feeds")
      await route.fulfill({ json: { feeds: [] } });
    else await route.fulfill({ json: item });
  });
  await page.route("**/archive/peek.html", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: '<p>Article body</p><img src="/media/e2e/lightbox-images/2.svg" width="1600" height="1000" alt="Second"><img src="/media/e2e/lightbox-images/3.svg" width="1600" height="1000" alt="Third">',
    }),
  );
  await page.goto("/");
  if (options.read)
    await page.getByRole("switch", { name: "Unread", exact: true }).uncheck();
  const cell = page.locator(
    options.story ? '[data-story-id="peek-story"]' : '[data-item-id="peek"]',
  );
  await expect(cell).toBeVisible();
  await cell.locator(".cell-main, .story-lead").first().focus();
  return { cell, mutations };
}
