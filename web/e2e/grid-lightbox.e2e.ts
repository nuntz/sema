import { expect, type Page, test } from "@playwright/test";

async function openGrid(page: Page, kind = "image", tag = false) {
  await page.addInitScript(() => localStorage.setItem("sema.signed-in", "1"));
  const item = {
    item_id: "peek",
    feed_id: "daily",
    feed_title: "Daily",
    url: "https://example.com/peek",
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
    size: "M",
    read: false,
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
            order_pref: "interest",
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
    } else if (path === "/api/items")
      await route.fulfill({ json: { items: [item], next_cursor: null } });
    else if (path === "/api/stories")
      await route.fulfill({ json: { stories: [] } });
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
  const cell = page.locator('[data-item-id="peek"]');
  await expect(cell).toBeVisible();
  await cell.locator(".cell-main").focus();
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

for (const method of ["Escape", "button", "Back"]) {
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
    if (method === "Escape") await page.keyboard.press("Escape");
    else if (method === "Back") await page.goBack();
    else
      await page
        .getByRole("button", { name: "Close lightbox", exact: true })
        .click();
    await expect(page.locator(".lb-overlay")).toHaveCount(0);
    await expect(page.locator(".scope-cell__title")).toHaveText("#design");
    await expect(cell.locator(".cell-main")).toBeFocused();
    expect(mutations).toEqual([]);
    // After dismissal, Escape should still clear the filter normally.
    await page.keyboard.press("Escape");
    await expect(page.locator(".scope-cell__title")).not.toHaveText("#design");
  });
}
