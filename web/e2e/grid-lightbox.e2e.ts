import { expect, type Page, test } from "@playwright/test";
import { stubYouTube } from "./youtube-stub";

async function openGrid(
  page: Page,
  kind = "image",
  tag = false,
  size = "M",
  options: {
    story?: boolean;
    second?: boolean;
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

test("grid peek grows, navigates, and restores focus without marking read", async ({
  page,
}) => {
  const { cell, mutations } = await openGrid(page);
  let release = () => {};
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/archive/peek.html", async (route) => {
    await pending;
    await route.fallback();
  });
  await page.keyboard.press("i");
  await expect(page.locator(".lb-overlay")).toBeVisible();
  await expect(page.locator(".lb-image").first()).toHaveAttribute(
    "src",
    /1.svg$/,
  );
  await expect(page.locator(".lb-counter")).toHaveCount(0);
  release();
  await expect(page.locator(".lb-counter")).toHaveText("1 / 3");
  await page.keyboard.press("ArrowRight");
  await expect(page.locator(".lb-counter")).toHaveText("2 / 3");
  await expect(page.locator(".lb-image").first()).toHaveAttribute(
    "src",
    /2.svg$/,
  );
  await page.keyboard.press("m");
  await page.keyboard.press("f");
  await page.keyboard.press("Escape");
  await expect(page.locator(".lb-overlay")).toHaveCount(0);
  await expect(cell.locator(".cell-main")).toBeFocused();
  await expect(cell).not.toHaveClass(/is-read/);
  expect(mutations).toEqual([]);
});

test("video without a body has no image action", async ({ page }) => {
  await openGrid(page, "video");
  await page.keyboard.press("i");
  await expect(page.locator(".lb-overlay")).toHaveCount(0);
  await page.keyboard.press("Shift+F10");
  await expect(page.locator(".action-sheet-layer")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "View images", exact: true }),
  ).toHaveCount(0);
});

for (const kind of ["image", "body"]) {
  test(`action sheet opens ${kind} images and returns focus`, async ({
    page,
  }) => {
    if (kind === "body")
      await page.setViewportSize({ width: 390, height: 844 });
    const { cell, mutations } = await openGrid(page, kind);
    await page.keyboard.press("Shift+F10");
    await page
      .locator(".action-sheet-layer")
      .getByRole("button", { name: "View images", exact: true })
      .click();
    await expect(page.locator(".lb-overlay")).toBeVisible();
    await expect(page.locator(".lb-counter")).toHaveText(
      kind === "body" ? "1 / 2" : "1 / 3",
    );
    await page.screenshot({
      path: `/tmp/sema-grid-lightbox-${kind}.png`,
      animations: "disabled",
    });
    await page.keyboard.press("Escape");
    await expect(cell.locator(".cell-main")).toBeFocused();
    expect(mutations).toEqual([]);
  });
}

for (const method of ["key", "button"]) {
  test(`flip by ${method} opens reader and Back returns to grid`, async ({
    page,
  }) => {
    const { cell } = await openGrid(page);
    await page.keyboard.press("i");
    await expect(page.locator(".lb-overlay")).toBeVisible();
    if (method === "key") await page.keyboard.press("o");
    else
      await page
        .getByRole("button", { name: "Open in reader", exact: true })
        .click();
    await expect(page.locator(".lb-overlay")).toHaveCount(0);
    await expect(page.locator(".reader")).toBeVisible();
    await page.goBack();
    await expect(page.locator(".reader")).toHaveCount(0);
    await expect(page.locator(".lb-overlay")).toHaveCount(0);
    await expect(cell).toBeVisible();
    await expect(cell).toHaveClass(/is-read/);
  });
}

test("reader lightbox does not offer or respond to the reader flip", async ({
  page,
}) => {
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
  await page.locator(".lb-openable").first().click();
  await expect(
    page.getByRole("button", { name: "Open in reader", exact: true }),
  ).toHaveCount(0);
  await page.keyboard.press("o");
  await expect(page.locator(".lb-overlay")).toBeVisible();
  await page.keyboard.press("?");
  await expect(page.locator(".lb-help")).not.toContainText("Open in reader");
});

test("closing a peek aborts its pending body and a late response cannot reopen it", async ({
  page,
}) => {
  const { cell, mutations } = await openGrid(page);
  let release = () => {};
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/archive/peek.html", async (route) => {
    await pending;
    await route.fallback();
  });
  const requested = page.waitForRequest("**/archive/peek.html");
  await page.keyboard.press("i");
  await requested;
  const aborted = page.waitForEvent("requestfailed", {
    predicate: (request) => request.url().endsWith("/archive/peek.html"),
  });
  await page.keyboard.press("Escape");
  await aborted;
  release();
  await expect(page.locator(".lb-overlay")).toHaveCount(0);
  await expect(cell.locator(".cell-main")).toBeFocused();
  expect(mutations).toEqual([]);
});

for (const declared of [true, false]) {
  test(`lazy detached body image supports zoom with declared dimensions ${declared}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await openGrid(page, "body");
    await page.route("**/archive/peek.html", (route) =>
      route.fulfill({
        contentType: "text/html",
        body: `<img loading="lazy" src="/media/portrait.svg" ${declared ? 'width="1600" height="2400"' : ""}>`,
      }),
    );
    let release = () => {};
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route("**/media/portrait.svg", async (route) => {
      await pending;
      await route.fulfill({
        contentType: "image/svg+xml",
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="1600" height="2400"><path fill="#52697c" d="M0 0h1600v2400H0z"/></svg>',
      });
    });
    await page.keyboard.press("i");
    await expect(page.locator(".lb-overlay")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Zoom in", exact: true }),
    ).toHaveCount(0);
    release();
    await expect(
      page.getByRole("button", { name: "Zoom in", exact: true }),
    ).toBeVisible();
    await expect
      .poll(async () => {
        const box = await page.locator(".lb-frame").boundingBox();
        return box ? Math.round((box.width / box.height) * 100) : 0;
      })
      .toBe(67);
    await page.keyboard.press("z");
    await expect(
      page.getByRole("button", { name: "Zoom out", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".lb-failure")).toHaveCount(0);
  });
}

for (const method of ["Escape", "i", "button", "Back"]) {
  test(`closing grid lightbox with ${method} preserves the tag filter`, async ({
    page,
  }) => {
    const { cell, mutations } = await openGrid(page, "image", true);
    await expect(page.locator(".scope-cell__title")).toHaveText("#design");
    await page.keyboard.press("i");
    await expect(page.locator(".lb-overlay")).toBeVisible();
    if (method !== "Escape") {
      await page.keyboard.press("#");
      await expect(page.locator(".grid-tag-filter")).not.toHaveClass(/is-open/);
    }
    if (method === "Escape" || method === "i")
      await page.keyboard.press(method);
    else if (method === "Back") await page.goBack();
    else
      await page
        .getByRole("button", { name: "Close lightbox", exact: true })
        .click();
    await expect(page.locator(".lb-overlay")).toHaveCount(0);
    await expect(page.locator(".scope-cell__title")).toHaveText("#design");
    await expect(cell.locator(".cell-main")).toBeFocused();
    expect(mutations).toEqual([]);
    if (method === "i") {
      await page.keyboard.press("i");
      await expect(page.locator(".lb-overlay")).toBeVisible();
      await page.keyboard.press("i");
      await expect(page.locator(".lb-overlay")).toHaveCount(0);
      await expect(cell.locator(".cell-main")).toBeFocused();
    }
    // After dismissal, Escape should still clear the filter normally.
    await page.keyboard.press("Escape");
    await expect(page.locator(".scope-cell__title")).not.toHaveText("#design");
  });
}

for (const width of [390, 1280]) {
  for (const size of ["S", "M", "L"]) {
    for (const kind of ["image", "video", "body"]) {
      test(`Peek pill eligibility: ${kind}, ${size}, ${width}px`, async ({
        page,
      }) => {
        await page.setViewportSize({ width, height: 900 });
        const { cell } = await openGrid(page, kind, false, size);
        await expect(
          cell.getByRole("button", { name: "View images", exact: true }),
        ).toHaveCount(kind === "image" && size !== "S" ? 1 : 0);
      });
    }
  }
}

for (const size of ["M", "L"]) {
  test(`Story Peek pill opens only the Lead: ${size}`, async ({ page }) => {
    const { cell, mutations } = await openGrid(page, "image", false, size, {
      story: true,
    });
    const pill = cell.getByRole("button", { name: "View images", exact: true });
    await expect(pill).toHaveCount(1);
    if (size === "L")
      await expect(cell.locator(".story-headline")).toHaveCount(1);
    await expect(cell.locator(".story-headline .peek-pill")).toHaveCount(0);
    await cell.hover();
    await pill.click();
    await expect(page.locator(".lb-overlay")).toBeVisible();
    await expect(page.locator(".lb-image").first()).toHaveAttribute(
      "src",
      /1.svg$/,
    );
    await page.keyboard.press("Escape");
    await expect(cell.locator(".cell-main, .story-lead").first()).toBeFocused();
    expect(mutations).toEqual([]);
  });
}

test("desktop Peek pill reveals on hover and keyboard focus", async ({
  page,
}) => {
  const { cell, mutations } = await openGrid(page);
  const pill = cell.getByRole("button", { name: "View images", exact: true });
  await page.mouse.move(0, 0);
  await expect(pill).toHaveCSS("opacity", "0");
  await cell.hover();
  await expect(pill).toHaveCSS("opacity", "1");
  await page.screenshot({
    path: "/tmp/sema-peek-pill-desktop.png",
    animations: "disabled",
  });
  await page.mouse.move(0, 0);
  await cell.locator(".cell-main").focus();
  // The Feed shortcut in the cell copy precedes the per-cell actions.
  for (let step = 0; step < 6; step++) {
    await page.keyboard.press("Tab");
    if (await pill.evaluate((button) => button === document.activeElement))
      break;
  }
  await expect(pill).toBeFocused();
  await expect(pill).toHaveCSS("opacity", "1");
  await page.keyboard.press("Enter");
  await expect(page.locator(".lb-overlay")).toBeVisible();
  await page.keyboard.press("Escape");
  expect(mutations).toEqual([]);
});

test.describe("touch Peek pill", () => {
  test.use({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
  });

  test("tap moves a stale Cursor and opens that Item without Read or Behaviour", async ({
    page,
  }) => {
    const { cell, mutations } = await openGrid(page, "image", false, "M", {
      second: true,
    });
    const other = page.locator('[data-item-id="other"]');
    await expect(cell).toHaveClass(/focused/);
    const pill = other.getByRole("button", {
      name: "View images",
      exact: true,
    });
    await expect(pill).toHaveCSS("opacity", "1");
    const box = await pill.boundingBox();
    expect(box?.width).toBeGreaterThanOrEqual(44);
    expect(box?.height).toBeGreaterThanOrEqual(44);
    await page.screenshot({
      path: "/tmp/sema-peek-pill-phone.png",
      animations: "disabled",
    });
    // Hold beyond both the pressed-state delay and the long-press threshold.
    const session = await page.context().newCDPSession(page);
    if (!box) throw new Error("Missing Peek pill bounds");
    await session.send("Input.dispatchTouchEvent", {
      type: "touchStart",
      touchPoints: [{ x: box.x + 2, y: box.y + 2 }],
    });
    await page.waitForTimeout(650);
    await expect(other).not.toHaveClass(/pressed/);
    await expect(page.locator(".action-sheet-layer")).toHaveCount(0);
    await session.send("Input.dispatchTouchEvent", {
      type: "touchEnd",
      touchPoints: [],
    });
    await expect(page.locator(".lb-overlay")).toBeVisible();
    await expect(page.locator(".lb-image").first()).toHaveAttribute(
      "src",
      /2.svg$/,
    );
    await expect(page.locator(".reader")).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(other).toHaveClass(/focused/);
    await expect(other.locator(".cell-main")).toBeFocused();
    await expect(other).not.toHaveClass(/is-read/);
    expect(mutations).toEqual([]);
  });

  test("Peek pill does not follow an external primary route", async ({
    page,
  }) => {
    const { cell, mutations } = await openGrid(page, "image", false, "M", {
      external: true,
    });
    await expect(cell.locator("a.cell-main")).toHaveAttribute(
      "target",
      "_blank",
    );
    await cell.getByRole("button", { name: "View images", exact: true }).tap();
    await expect(page.locator(".lb-overlay")).toBeVisible();
    expect(page.context().pages()).toHaveLength(1);
    await expect(page.locator(".reader")).toHaveCount(0);
    expect(mutations).toEqual([]);
  });
});

for (const view of ["Front Page", "chrono", "tag", "Archive"]) {
  test(`Read cells retain their Peek pill in ${view}`, async ({ page }) => {
    const { cell, mutations } = await openGrid(
      page,
      "image",
      view === "tag",
      "M",
      {
        read: true,
        order: view === "chrono" ? "chrono" : "interest",
      },
    );
    if (view === "Archive") {
      await page.keyboard.press("Shift+A");
      await expect(cell).toHaveClass(/archive-cell/);
    }
    await cell.hover();
    const before = mutations.length;
    await cell
      .getByRole("button", { name: "View images", exact: true })
      .click();
    await expect(page.locator(".lb-overlay")).toBeVisible();
    await expect(page.locator(".reader")).toHaveCount(0);
    await page.keyboard.press("Escape");
    expect(mutations.slice(before)).toEqual([]);
  });
}

for (const entry of ["thumbnail", "key", "pill", "sheet", "story"]) {
  test(`Video Item Peek from ${entry} records Play without Read, then Flips with position`, async ({
    page,
  }) => {
    await stubYouTube(page);
    const events: string[] = [];
    page.on("request", (request) => {
      if (request.method() === "POST" && request.url().includes("/api/"))
        events.push(request.postData() || "");
    });
    const { cell } = await openGrid(page, "youtube", false, "M", {
      story: entry === "story",
    });
    if (entry === "thumbnail")
      await cell.locator(".cell-main").click({ position: { x: 50, y: 50 } });
    else if (entry === "pill" || entry === "story") {
      await cell.hover();
      await cell.locator(".peek-pill").click();
    } else if (entry === "sheet") {
      await page.keyboard.press("Shift+F10");
      await page
        .locator(".action-sheet-layer")
        .getByRole("button", { name: "Play", exact: true })
        .click();
    } else await page.keyboard.press("i");
    await expect(page.locator(".video-peek iframe")).toBeVisible();
    await expect(cell).not.toHaveClass(/is-read/);
    await expect(page.locator(".reader")).toHaveCount(0);
    await expect
      .poll(() => events.join(" "), { timeout: 10_000 })
      .toContain('"clicked_through":true');
    expect(events.join(" ")).not.toContain('"opened":true');
    await page.keyboard.press("o");
    await expect(page.locator(".video-peek")).toHaveCount(0);
    await expect(page.locator(".reader iframe")).toHaveAttribute(
      "data-seconds",
      "87",
    );
    await expect(page.locator(".video-media-band")).toHaveCount(0);
    await page.screenshot({ path: `/tmp/sema-video-flip-${entry}.png` });
  });
}

test("Video Item text opens the Poster; Play failure stays in the frame", async ({
  page,
}) => {
  await stubYouTube(page, true);
  const { cell } = await openGrid(page, "youtube");
  await cell.locator(".video-reader-link").first().click();
  await expect(page.locator(".video-media-card")).toBeVisible();
  await expect(page.locator(".article-body")).toContainText("Article body");
  await page.keyboard.press("i");
  await expect(page.locator(".video-failure")).toContainText(
    "Plays on YouTube only",
  );
  await expect(page.locator(".video-failure a")).toHaveAttribute(
    "href",
    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
  );
  await expect(page.locator(".video-media-band")).toHaveCount(0);
  expect(page.context().pages()).toHaveLength(1);
});

for (const key of ["Escape", "i"]) {
  test(`Video Peek closes and destroys playback with ${key}`, async ({
    page,
  }) => {
    await stubYouTube(page);
    const { cell } = await openGrid(page, "youtube");
    await page.keyboard.press("i");
    await expect(page.locator(".video-peek iframe")).toBeVisible();
    await page.locator(".video-peek iframe").focus();
    await page.keyboard.press("Shift+Tab");
    await expect(page.locator(".video-peek")).toBeFocused();
    await page.keyboard.press(key);
    await expect(page.locator(".video-peek")).toHaveCount(0);
    await expect(cell.locator(".cell-main")).toBeFocused();
    await expect(cell).not.toHaveClass(/is-read/);
  });
}

for (const control of ["Seek", "Pause"]) {
  for (const key of ["Escape", "i", "o"]) {
    test(`${key} works in Video Peek after seeking or pausing with ${control}`, async ({
      page,
    }) => {
      await stubYouTube(page, false, true);
      const { cell } = await openGrid(page, "youtube");
      await page.keyboard.press("i");
      await page
        .frameLocator(".video-peek iframe")
        .getByRole("button", { name: control })
        .click();
      await expect(page.locator(".video-peek iframe")).toHaveAttribute(
        "data-state",
        control === "Seek" ? "1" : "2",
      );
      await page.keyboard.press(key);
      await expect(page.locator(".video-peek")).toHaveCount(0);
      if (key === "o")
        await expect(page.locator(".reader iframe")).toHaveAttribute(
          "data-seconds",
          "87",
        );
      else {
        await expect(cell.locator(".cell-main")).toBeFocused();
        await expect(cell).not.toHaveClass(/is-read/);
      }
    });
  }
}

test("Escape closes Video Peek after leaving other YouTube controls", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await stubYouTube(page, false, true);
  await openGrid(page, "youtube");
  await page.keyboard.press("i");
  await page
    .frameLocator(".video-peek iframe")
    .getByRole("button", { name: "Mute" })
    .click();
  await page.mouse.move(1, 1);
  await page.keyboard.press("Escape");
  await expect(page.locator(".video-peek")).toHaveCount(0);
});
