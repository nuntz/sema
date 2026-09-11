import { expect, test } from "@playwright/test";

for (const { outcome, width } of [
  { outcome: "loaded", width: 1280 },
  { outcome: "loaded", width: 390 },
  { outcome: "failed", width: 1280 },
  { outcome: "navigated", width: 1280 },
] as const) {
  test(`reader displays its final image once: ${outcome} at ${width}px`, async ({
    page,
  }, testInfo) => {
    let releaseImage = () => {};
    const pendingImage = new Promise<void>((resolve) => {
      releaseImage = resolve;
    });
    const svg =
      '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"><rect width="640" height="360" fill="red"/></svg>';
    await page.route("**/media/e2e/reader-media/*.svg", (route) =>
      route.fulfill({ contentType: "image/svg+xml", body: svg }),
    );
    let bodyRequests = 0;
    await page.route("**/e2e/reader-body.html", (route) =>
      route.fulfill({
        contentType: "text/html",
        body:
          bodyRequests++ === 0
            ? '<figure><img src="/media/e2e/handoff.svg" width="640" height="480" loading="lazy"></figure><p>First article body</p>'
            : "<p>Next article body</p>",
      }),
    );
    await page.route("**/media/e2e/handoff.svg", async (route) => {
      await pendingImage;
      if (outcome === "failed") await route.abort();
      else
        await route.fulfill({
          contentType: "image/svg+xml",
          body: svg.replaceAll("360", "480"),
        });
    });
    try {
      await page.emulateMedia({ reducedMotion: "reduce" });
      await page.setViewportSize({ width, height: 900 });
      await page.addInitScript(() => {
        const state = window as typeof window & {
          decodedImages: HTMLImageElement[];
          temporaryLeads: number;
        };
        state.decodedImages = [];
        state.temporaryLeads = 0;
        const decode = HTMLImageElement.prototype.decode;
        HTMLImageElement.prototype.decode = async function () {
          await decode.call(this);
          state.decodedImages.push(this);
        };
        new MutationObserver((records) => {
          for (const record of records)
            for (const node of record.addedNodes) {
              if (
                node instanceof Element &&
                (node.matches(".article-lead") ||
                  node.querySelector(".article-lead"))
              )
                state.temporaryLeads++;
            }
        }).observe(document, { childList: true, subtree: true });
      });
      const requested = page.waitForRequest("**/media/e2e/handoff.svg");
      await page.goto("/e2e/header-fixture.html?view=reader&media=article");
      await requested;
      const lead = page.locator(".article-lead");
      await expect(lead).toHaveCount(0);
      await expect(page.locator(".extraction-loading")).toBeVisible();
      await expect(page.locator(".article-body")).toHaveCount(0);
      if (outcome === "navigated") {
        await page.keyboard.press("n");
        await expect(page.locator(".article-body")).toHaveText(
          "Next article body",
        );
      }
      const finished =
        outcome === "navigated"
          ? page.waitForResponse("**/media/e2e/handoff.svg")
          : undefined;
      releaseImage();
      if (outcome === "navigated") {
        await finished;
        await page.evaluate(() => new Promise(requestAnimationFrame));
        await expect(page.locator(".article-body")).toHaveText(
          "Next article body",
        );
        await expect(lead).toHaveAttribute(
          "src",
          "/media/e2e/reader-media/1.svg",
        );
      } else {
        await expect(page.locator(".article-body")).toContainText(
          "First article body",
        );
        await expect(lead).toHaveCount(0);
        if (outcome === "loaded") {
          const finalBounds = await page
            .locator(".article-body img")
            .boundingBox();
          expect(finalBounds?.width).toBeCloseTo(Math.min(640, width - 44), 0);
          expect(finalBounds?.height).toBeCloseTo(
            ((finalBounds?.width ?? 0) * 480) / 640,
            0,
          );
          expect(
            await page.locator(".article-body img").evaluate((image) =>
              (
                window as typeof window & {
                  decodedImages: HTMLImageElement[];
                }
              ).decodedImages.includes(image as HTMLImageElement),
            ),
          ).toBe(true);
          expect(
            await page.evaluate(
              () =>
                (window as typeof window & { temporaryLeads: number })
                  .temporaryLeads,
            ),
          ).toBe(0);
          await page.screenshot({
            path: testInfo.outputPath("reader-handoff.png"),
          });
          expect(
            await page
              .locator(".article-body img")
              .evaluate(
                (element: HTMLImageElement) =>
                  element.complete && element.naturalWidth > 0,
              ),
          ).toBe(true);
          expect(
            await page.locator(".article-body img").evaluate((image) => {
              const wrapper = image.parentElement;
              image.dispatchEvent(new Event("load"));
              return image.parentElement === wrapper;
            }),
          ).toBe(true);
        }
      }
    } finally {
      releaseImage();
    }
  });
}

for (const body of ["", "<p>Text-only article</p>", null]) {
  test(`reader shows the stored lead after resolving a body without a leading image: ${body}`, async ({
    page,
  }) => {
    let releaseBody = () => {};
    const pendingBody = new Promise<void>((resolve) => {
      releaseBody = resolve;
    });
    await page.route("**/e2e/reader-body.html", async (route) => {
      await pendingBody;
      await route.fulfill({
        status: body === null ? 500 : 200,
        contentType: "text/html",
        body: body ?? "",
      });
    });
    await page.route("**/media/e2e/reader-media/*.svg", (route) =>
      route.fulfill({
        contentType: "image/svg+xml",
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"/>',
      }),
    );
    try {
      await page.goto("/e2e/header-fixture.html?view=reader&media=article");
      await expect(page.locator(".extraction-loading")).toBeVisible();
      await expect(page.locator(".article-lead")).toHaveCount(0);
      releaseBody();
      await expect(page.locator(".article-lead")).toBeVisible();
      await expect(page.locator(".extraction-loading")).toHaveCount(0);
      if (body)
        await expect(page.locator(".article-body")).toHaveText(
          "Text-only article",
        );
      else await expect(page.locator(".article-body")).toHaveCount(0);
    } finally {
      releaseBody();
    }
  });
}

for (const { media, width } of [
  { media: "article", width: 1280 },
  { media: "article", width: 390 },
  { media: "video", width: 1280 },
  { media: "video", width: 390 },
]) {
  test(`reader replaces ${media} media at ${width}px while the next image loads`, async ({
    page,
  }, testInfo) => {
    let releaseImage = () => {};
    const pendingImage = new Promise<void>((resolve) => {
      releaseImage = resolve;
    });
    await page.route("**/media/e2e/reader-media/*.svg", async (route) => {
      const next = new URL(route.request().url()).pathname.includes("/1");
      if (next) await pendingImage;
      await route.fulfill({
        contentType: "image/svg+xml",
        body: `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"><rect width="640" height="360" fill="${next ? "blue" : "red"}"/></svg>`,
      });
    });

    try {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(`/e2e/header-fixture.html?view=reader&media=${media}`);
      const reader = page.locator(".reader");
      const image = reader.locator(
        media === "article" ? ".article-lead" : ".video-media-band img",
      );
      await image.evaluate((element: HTMLImageElement) => element.decode());
      const previousImage = await image.elementHandle();
      if (!previousImage) throw new Error("Reader image is missing");

      // Updating an item in place must preserve its already loaded media.
      await page.keyboard.press(".");
      await expect(
        reader.locator(".chrome-group--judge button").first(),
      ).toHaveAttribute("aria-pressed", "true");
      expect(
        await image.evaluate(
          (element, previous) => element === previous,
          previousImage,
        ),
      ).toBe(true);

      await page.keyboard.press("n");
      await expect(reader.locator("h1")).toHaveText("Reader media item 1");
      await expect(reader.locator(".article-summary")).toContainText(
        "Summary for reader media item 1.",
      );
      expect(
        await previousImage.evaluate((element) => element.isConnected),
      ).toBe(false);
      expect(await previousImage.getAttribute("src")).toBeNull();
      expect(await previousImage.getAttribute("srcset")).toBeNull();
      await expect(image).toHaveAttribute(
        "src",
        "/media/e2e/reader-media/1.svg",
      );
      expect(
        await image.evaluate(
          (element: HTMLImageElement) => element.naturalWidth,
        ),
      ).toBe(0);
      const bounds = await image.boundingBox();
      expect(bounds?.width).toBeGreaterThan(0);
      expect(bounds?.height).toBeGreaterThan(0);
      await page.screenshot({
        path: testInfo.outputPath("next-image-pending.png"),
        animations: "disabled",
      });

      releaseImage();
      await expect
        .poll(() =>
          image.evaluate(
            (element: HTMLImageElement) =>
              element.complete && element.naturalWidth > 0,
          ),
        )
        .toBe(true);
      expect(
        await image.evaluate((element: HTMLImageElement) => element.currentSrc),
      ).toContain("/media/e2e/reader-media/1-");
    } finally {
      releaseImage();
    }
  });
}
