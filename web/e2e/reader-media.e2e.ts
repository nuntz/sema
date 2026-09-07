import { expect, test } from "@playwright/test";

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
    await page.route("**/e2e/reader-media/*.svg", async (route) => {
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
      await expect(image).toHaveAttribute("src", "/e2e/reader-media/1.svg");
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
      ).toContain("/e2e/reader-media/1-");
    } finally {
      releaseImage();
    }
  });
}
