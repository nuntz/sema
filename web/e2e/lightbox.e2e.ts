import { expect, test } from "@playwright/test";

for (const width of [1280, 390]) {
  test(`reader lightbox navigation, history and focus at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.emulateMedia({ colorScheme: "dark" });
    await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
    const images = page.locator(".article-body img.lb-openable");
    await expect(images).toHaveCount(3);
    await images.first().scrollIntoViewIfNeeded();
    await images.first().click();
    const lightbox = page.locator(".lb-overlay");
    await expect(lightbox).toBeVisible();
    await expect(page.locator("#root")).toHaveAttribute("inert", "");
    await expect(page.locator(".lb-counter")).toHaveText("1 / 3");
    if (width === 1280)
      await expect(
        page.getByRole("button", { name: "Previous image", exact: true }),
      ).toHaveAttribute("aria-disabled", "true");
    for (const key of [
      "n",
      "p",
      "Space",
      "PageDown",
      "PageUp",
      "v",
      "+",
      "-",
      "f",
      "r",
      "c",
    ])
      await page.keyboard.press(key);
    await expect(page.locator(".lb-counter")).toHaveText("1 / 3");
    await page.keyboard.press("j");
    await expect(page.locator(".lb-counter")).toHaveText("2 / 3");
    await expect(page.locator(".lb-frame .lb-image").first()).toHaveAttribute(
      "src",
      /\/2\.svg$/,
    );
    await page.keyboard.press("k");
    await expect(page.locator(".lb-counter")).toHaveText("1 / 3");
    await page.keyboard.press("End");
    await expect(page.locator(".lb-counter")).toHaveText("3 / 3");
    await page.keyboard.press("j");
    await expect(page.locator(".lb-counter")).toHaveText("3 / 3");
    if (width === 1280)
      await expect(
        page.getByRole("button", { name: "Next image", exact: true }),
      ).toHaveAttribute("aria-disabled", "true");
    await page.keyboard.press("Home");
    await page.keyboard.press("Tab");
    expect(
      await page.evaluate(
        () => !!document.activeElement?.closest(".lb-overlay"),
      ),
    ).toBe(true);
    await lightbox.evaluate(async (element) => {
      await Promise.allSettled(
        element
          .getAnimations({ subtree: true })
          .filter(
            (animation) =>
              animation.effect?.getTiming().iterations !== Infinity,
          )
          .map((animation) => animation.finished),
      );
    });
    await page.screenshot({ path: `/tmp/sema-lightbox-${width}.png` });
    await page.keyboard.press("Escape");
    await expect(lightbox).toHaveCount(0);
    await expect(page.locator(".reader")).toBeVisible();
    await expect(images.first()).toBeFocused();
    if (width === 390) {
      await images.first().press("Enter");
      await expect(lightbox).toBeVisible();
      await page.goBack();
      await expect(lightbox).toHaveCount(0);
      await expect(page.locator(".reader")).toBeVisible();
      await expect(images.first()).toBeFocused();
    }
    await page.keyboard.press("Escape");
    await expect(page.locator(".reader")).toHaveCount(0);
  });
}

test("desktop zoom, help, body failure and inline keyboard entry", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
  const origin = page.locator(".lb-openable").first();
  await origin.scrollIntoViewIfNeeded();
  await origin.focus();
  await origin.press("Space");
  const overlay = page.locator(".lb-overlay");
  await expect(overlay).toBeVisible();
  await expect(page.locator(".lb-progress")).toHaveCount(0);
  await expect(page.locator(".lb-soft")).toHaveCount(0);
  await page.keyboard.press("z");
  await expect(overlay).toHaveAttribute("data-zoom", "");
  await expect(page.locator(".lb-ratio")).toHaveText("1:1");
  await expect(page.locator(".lb-zone")).toHaveCount(0);
  await page.keyboard.press("j");
  await expect(overlay).not.toHaveAttribute("data-zoom", "");
  await page.keyboard.press("?");
  await expect(page.locator(".lb-help")).toContainText(
    "Reader keys are suspended",
  );
  await page.keyboard.press("Escape");
  await expect(overlay).toHaveCount(0);
  await expect(origin).toBeFocused();
  await page.route("**/lightbox-images/1.svg", (route) => route.abort());
  await page.reload();
  await page.locator(".lb-openable").first().click();
  await expect(page.locator(".lb-failure")).toContainText(
    "Image could not be loaded",
  );
  await expect(page.locator(".lb-failure button")).toHaveText("Open original");
});

test("phone paging flips the counter mid-swipe and downward drag dismisses", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
  const origin = page.locator(".lb-openable").first();
  await origin.click();
  const overlay = page.locator(".lb-overlay");
  await expect(overlay).toBeVisible();
  await page.mouse.move(310, 420);
  await page.mouse.down();
  await page.mouse.move(60, 420, { steps: 8 });
  await expect(page.locator(".lb-counter")).toHaveText("2 / 3");
  await page.mouse.up();
  await expect(page.locator(".lb-caption")).toContainText("view 2");
  await page.mouse.move(195, 400);
  await page.mouse.down();
  await page.mouse.move(195, 550, { steps: 8 });
  await page.mouse.up();
  await expect(overlay).toHaveCount(0);
  await expect(origin).toBeFocused();
  await expect(page.locator(".reader")).toBeVisible();
});

test("lead quality swap preserves geometry and failures retain the soft image", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  let release = () => {};
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/reader-media/*.svg", async (route) => {
    if (route.request().url().endsWith("-2000.svg")) await pending;
    await route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="2000" height="1125"><path fill="#52697c" d="M0 0h2000v1125H0z"/></svg>',
    });
  });
  await page.goto(
    "/e2e/header-fixture.html?view=reader&lightbox=1&media=image",
  );
  const lead = page.locator(".article-lead");
  await expect
    .poll(() =>
      lead.evaluate((image) => (image as HTMLImageElement).naturalWidth),
    )
    .toBeGreaterThan(0);
  await lead.click();
  await expect(page.locator(".lb-progress")).toHaveCount(1);
  const geometry = await page.locator(".lb-frame").getAttribute("style");
  release();
  await expect(page.locator(".lb-sharp")).toHaveCount(1);
  await expect(page.locator(".lb-progress")).toHaveCount(0);
  await expect(page.locator(".lb-frame")).toHaveAttribute(
    "style",
    geometry ?? "",
  );
  await page.keyboard.press("Escape");
  await expect(page.locator(".lb-overlay")).toHaveCount(0);
  await page.route("**/*-2000.svg", (route) => route.abort());
  await lead.click();
  await expect(page.locator(".lb-progress")).toHaveCount(0);
  await expect(page.locator(".lb-soft")).toHaveCount(1);
  await expect(page.locator(".lb-failure")).toHaveCount(0);
});

test("phone pinch and double tap zoom while navigation stays suspended", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
  await page.locator(".lb-openable").first().click();
  const session = await page.context().newCDPSession(page);
  const touch = async (
    type: "touchStart" | "touchMove" | "touchEnd",
    points: { x: number; y: number; id: number }[],
  ) => session.send("Input.dispatchTouchEvent", { type, touchPoints: points });
  await touch("touchStart", [
    { x: 160, y: 420, id: 0 },
    { x: 230, y: 420, id: 1 },
  ]);
  await touch("touchMove", [
    { x: 125, y: 420, id: 0 },
    { x: 265, y: 420, id: 1 },
  ]);
  await touch("touchEnd", []);
  await expect(page.locator(".lb-counter")).toHaveText("1 / 3 · 2.0×");
  await page.mouse.move(300, 420);
  await page.mouse.down();
  await page.mouse.move(40, 420, { steps: 8 });
  await page.mouse.up();
  await expect(page.locator(".lb-counter")).toHaveText("1 / 3 · 2.0×");
  await page.keyboard.press("j");
  await expect(page.locator(".lb-counter")).toHaveText("2 / 3");
  await page.mouse.dblclick(195, 420);
  await expect(page.locator(".lb-counter")).toHaveText("2 / 3 · 2.0×");
});

test("single-image reduced-motion view omits navigation and returns focus", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "light" });
  await page.route("**/reader-body.html", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: '<img src="/media/e2e/lightbox-images/1.svg" width="1600" height="1000" alt="Only image">',
    }),
  );
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
  const origin = page.locator(".lb-openable");
  await origin.click();
  await expect(page.locator(".lb-counter, .lb-zone, .lb-dots")).toHaveCount(0);
  await page.keyboard.press("j");
  await expect(page.locator(".lb-image")).toHaveAttribute("alt", "Only image");
  await page.keyboard.press("Escape");
  await expect(origin).toBeFocused();
});

test("filmstrip keeps the selection visible and survives phone chrome idle", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.route("**/reader-body.html", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: Array.from(
        { length: 12 },
        (_, index) =>
          `<img src="/media/e2e/lightbox-images/1.svg?image=${index}" width="1600" height="1000" alt="Image ${index + 1}">`,
      ).join(""),
    }),
  );
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
  await page.locator(".lb-openable").first().click();
  await expect(page.locator(".lb-strip button")).toHaveCount(12);
  await page.keyboard.press("End");
  await expect(page.locator(".lb-strip button").last()).toHaveAttribute(
    "aria-current",
    "true",
  );
  await expect(page.locator(".lb-strip button").last()).toBeInViewport();
  await expect(page.locator(".lb-overlay")).toHaveAttribute("data-idle", "", {
    timeout: 4000,
  });
  await expect(page.locator(".lb-counter")).toHaveCSS("opacity", "0.6");
});

test("unloaded body images remain reachable from the first image", async ({
  page,
}) => {
  let release = () => {};
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route("**/lightbox-images/2.svg", async (route) => {
    await pending;
    await route.continue();
  });
  await page.route("**/reader-body.html", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: '<img src="/media/e2e/lightbox-images/1.svg" width="1600" height="1000" alt="First"><p>Later image, still loading:</p><img src="/media/e2e/lightbox-images/2.svg" alt="Later">',
    }),
  );
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1", {
    waitUntil: "domcontentloaded",
  });
  const images = page.locator(".article-body img");
  await expect(images).toHaveCount(2);
  await expect
    .poll(() =>
      images
        .first()
        .evaluate((image) => (image as HTMLImageElement).naturalWidth),
    )
    .toBeGreaterThan(0);
  await expect(images.nth(1)).toHaveClass("lb-openable");
  await images.first().click();
  await expect(page.locator(".lb-counter")).toHaveText("1 / 2");
  release();
  await page.keyboard.press("j");
  await expect(page.locator(".lb-frame .lb-image").first()).toHaveAttribute(
    "src",
    /\/2\.svg$/,
  );
  await expect
    .poll(() =>
      page
        .locator(".lb-frame .lb-image")
        .first()
        .evaluate((image) => (image as HTMLImageElement).naturalWidth),
    )
    .toBeGreaterThan(0);
});

for (const width of [1280, 390]) {
  test(`linked cached image opens without navigation at ${width}px`, async ({
    page,
    context,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.route("**/reader-body.html", (route) =>
      route.fulfill({
        contentType: "text/html",
        body: '<a href="https://publisher.example/original.JPG?1788861996" target="_blank" rel="noopener noreferrer"><img src="/media/e2e/lightbox-images/1.svg" width="1600" height="1000" alt="Linked image"></a>',
      }),
    );
    await context.route("https://publisher.example/**", (route) =>
      route.fulfill({
        contentType: "text/html",
        body: "Original image destination",
      }),
    );
    await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
    const image = page.locator(".article-body img");
    await expect(image).toHaveClass("lb-openable");
    const popups: unknown[] = [];
    page.on("popup", (popup) => popups.push(popup));
    const pagesBefore = context.pages().length;
    await image.click();
    await expect(page.locator(".lb-overlay")).toBeVisible();
    await expect(page.locator(".lb-image")).toHaveAttribute(
      "src",
      /\/media\/e2e\/lightbox-images\/1.svg$/,
    );
    // Give any default anchor navigation time to create its target before asserting.
    await page.keyboard.press("Escape");
    await expect(page.locator(".lb-overlay")).toHaveCount(0);
    expect(popups).toHaveLength(0);
    expect(context.pages()).toHaveLength(pagesBefore);
    await expect(image).toBeFocused();
    for (const key of ["Enter", "Space"]) {
      await image.press(key);
      await expect(page.locator(".lb-overlay")).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(page.locator(".lb-overlay")).toHaveCount(0);
    }
    expect(popups).toHaveLength(0);
    const popupPromise = context.waitForEvent("page");
    await image.click({ modifiers: ["ControlOrMeta"] });
    const popup = await popupPromise;
    await expect(popup).toHaveURL(
      "https://publisher.example/original.JPG?1788861996",
    );
    await expect(page.locator(".lb-overlay")).toHaveCount(0);
    await popup.close();
    const middlePromise = context.waitForEvent("page");
    await image.click({ button: "middle" });
    const middle = await middlePromise;
    await expect(middle).toHaveURL(
      "https://publisher.example/original.JPG?1788861996",
    );
    await expect(page.locator(".lb-overlay")).toHaveCount(0);
  });
}

test("page links, mixed-content links and media cards retain native navigation", async ({
  page,
  context,
}) => {
  await page.route("**/reader-body.html", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: '<a href="https://publisher.example/article.html" target="_blank"><img src="/media/e2e/lightbox-images/1.svg" width="1600" height="1000" alt="Page link"></a><a href="https://publisher.example/photo.jpg">Read more<img src="/media/e2e/lightbox-images/2.svg" width="1600" height="1000"></a><a class="media-card" href="https://publisher.example/photo.jpg"><img src="/media/e2e/lightbox-images/3.svg" width="1600" height="1000"></a>',
    }),
  );
  await context.route("https://publisher.example/**", (route) =>
    route.fulfill({ contentType: "text/html", body: "Article destination" }),
  );
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
  const images = page.locator(".article-body img");
  await expect(images).toHaveCount(3);
  await expect(
    page.locator(".lb-openable, .lb-inline, .lb-hover-pill"),
  ).toHaveCount(0);
  for (const image of await images.all())
    await expect(image).not.toHaveAttribute("tabindex");
  const popupPromise = context.waitForEvent("page");
  await images.first().click();
  const popup = await popupPromise;
  await expect(popup).toHaveURL("https://publisher.example/article.html");
  await expect(page.locator(".lb-overlay")).toHaveCount(0);
});
