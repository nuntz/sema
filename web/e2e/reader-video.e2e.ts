import { expect, type Page, test } from "@playwright/test";
import { stubYouTube } from "./youtube-stub";

for (const width of [390, 1280]) {
  test(`reader Play and chapters seek in place at ${width}px`, async ({
    page,
  }) => {
    await stubYouTube(page);
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/e2e/header-fixture.html?view=reader&media=video");
    await expect(page.locator("iframe")).toHaveCount(0);
    expect(await page.locator('script[src*="youtube"]').count()).toBe(0);
    await page.getByRole("link", { name: "1:30", exact: true }).click();
    const player = page.locator(".video-embed iframe");
    await expect(player).toHaveAttribute("data-seconds", "90");
    await expect(page.locator("body")).toHaveAttribute("data-plays", "1");
    await expect(page.locator(".video-provider-strip")).toContainText("Open");
    await page.getByRole("link", { name: "3:45", exact: true }).click();
    await expect(player).toHaveAttribute("data-seconds", "225");
    await expect(player).toHaveCount(1);
    expect(page.context().pages()).toHaveLength(1);
    await page.screenshot({ path: `/tmp/sema-reader-video-${width}.png` });
    await page.keyboard.press("n");
    await expect(page.locator("iframe")).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Play", exact: true }),
    ).toBeVisible();
  });
}

test("Reddit YouTube link replaces destination card and leaves description unparsed", async ({
  page,
}) => {
  await stubYouTube(page);
  await page.goto(
    "/e2e/header-fixture.html?view=reader&media=video&reddit=link",
  );
  await expect(page.locator(".video-media-card")).toHaveCount(1);
  await expect(page.locator(".reddit-media-card")).toHaveCount(0);
  await expect(page.locator(".video-description")).toHaveCount(0);
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect(page.locator(".video-embed iframe")).toHaveCount(1);
  await expect(page.locator("body")).toHaveAttribute("data-plays", "1");
});

test("reader dwell continues after iframe blur and pauses on player pause", async ({
  page,
}) => {
  await stubYouTube(page);
  await page.clock.install();
  await page.goto("/e2e/header-fixture.html?view=reader&media=video");
  await page.keyboard.press("i");
  await expect(page.locator("iframe")).toHaveCount(1);
  await page.evaluate(() => window.dispatchEvent(new Event("blur")));
  await page.clock.runFor(31_000);
  const elapsed = Number(await page.locator("body").getAttribute("data-dwell"));
  expect(elapsed).toBeGreaterThanOrEqual(30_000);
  await page
    .locator("iframe")
    .evaluate((frame) =>
      frame.dispatchEvent(new CustomEvent("test-state", { detail: 2 })),
    );
  const paused = await page.locator("body").getAttribute("data-dwell");
  await page.clock.runFor(10_000);
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await page.clock.runFor(10_000);
  await page.keyboard.press("n");
  expect(await page.locator("body").getAttribute("data-dwell")).toBe(paused);
});

test("failed API loading shows Open without restoring the Poster", async ({
  page,
}) => {
  await page.route("https://www.youtube.com/iframe_api", (route) =>
    route.abort(),
  );
  await page.goto("/e2e/header-fixture.html?view=reader&media=video");
  await page.getByRole("button", { name: "Play", exact: true }).click();
  await expect(page.locator(".video-failure")).toContainText(
    "Plays on YouTube only",
  );
  await expect(page.locator(".video-media-band")).toHaveCount(0);
  await expect(page.locator("body")).toHaveAttribute("data-plays", "1");
});

const extractedVideo = (id: string, title: string) =>
  `<a class="media-card" href="https://www.youtube.com/watch?v=${id}" title="${title}" target="_blank"><span class="media-card-thumbnail"><img src="/media/e2e/reader-media/0.svg" alt="" loading="lazy"><span class="video-card-play" aria-hidden="true"></span></span><span class="video-provider-strip"><b>YOUTUBE</b><i></i><span>www.youtube.com/watch?v=${id}</span><strong>Watch<span class="media-card-open"></span></strong></span></a>`;

async function openArticleWithVideos(page: Page, failure = false) {
  await stubYouTube(page, failure);
  await page.route("**/e2e/reader-body.html", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: `<p>ArtStation project introduction.</p>${extractedVideo("dQw4w9WgXcQ", "Project breakdown")}<p>Work in progress.</p>${extractedVideo("abcdefghijk", "Second video")}<p>Project credits.</p><a href="https://youtu.be/dQw4w9WgXcQ">Ordinary YouTube link</a><a class="media-card" href="https://vimeo.com/1234">Vimeo video</a>`,
    }),
  );
  await page.goto(
    "/e2e/header-fixture.html?view=reader&media=article&lightbox=1",
  );
  await expect(page.locator(".reader-video")).toHaveCount(2);
}

for (const width of [390, 1280]) {
  test(`article YouTube cards Play in reader at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await openArticleWithVideos(page);
    await expect(page.locator("iframe")).toHaveCount(0);
    await expect(page.locator(".reader-video .lb-hover-pill")).toHaveCount(0);
    await expect(page.locator(".article-body")).toContainText(
      "Project credits.",
    );
    await expect(
      page.getByRole("link", { name: "Ordinary YouTube link" }),
    ).toHaveAttribute("href", "https://youtu.be/dQw4w9WgXcQ");
    await expect(page.getByRole("link", { name: "Vimeo video" })).toHaveClass(
      "media-card",
    );
    const poster = page.getByRole("button", {
      name: "Play Project breakdown",
      exact: true,
    });
    if (width === 1280) {
      await poster.focus();
      await page.keyboard.press("Enter");
    } else await poster.click();
    const player = page.locator(".reader-video iframe");
    await expect(player).toHaveAttribute("data-video-id", "dQw4w9WgXcQ");
    await expect(page.locator("body")).toHaveAttribute("data-plays", "1");
    await expect(
      page.locator(".reader-video .video-provider-strip").first(),
    ).toContainText("Open");
    expect(page.context().pages()).toHaveLength(1);
    const originalPlayer = await player.elementHandle();
    await page.keyboard.press(".");
    expect(
      await player.evaluate(
        (element, previous) => element === previous,
        originalPlayer,
      ),
    ).toBe(true);
    await page.screenshot({ path: `/tmp/sema-inline-video-${width}.png` });
    await page.keyboard.press("n");
    await expect(page.locator("iframe")).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Play Project breakdown", exact: true }),
    ).toBeVisible();
  });
}

test("article videos share reader dwell while any video keeps playing", async ({
  page,
}) => {
  await page.clock.install();
  await openArticleWithVideos(page);
  await page
    .getByRole("button", { name: "Play Project breakdown", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Play Second video", exact: true })
    .click();
  const players = page.locator(".reader-video iframe");
  await expect(players).toHaveCount(2);
  await expect(page.locator("body")).toHaveAttribute("data-plays", "2");
  await players
    .first()
    .evaluate((frame) =>
      frame.dispatchEvent(new CustomEvent("test-state", { detail: 2 })),
    );
  await page.evaluate(() => window.dispatchEvent(new Event("blur")));
  await page.clock.runFor(31_000);
  expect(
    Number(await page.locator("body").getAttribute("data-dwell")),
  ).toBeGreaterThanOrEqual(30_000);
  await players
    .last()
    .evaluate((frame) =>
      frame.dispatchEvent(new CustomEvent("test-state", { detail: 2 })),
    );
  const paused = await page.locator("body").getAttribute("data-dwell");
  await page.clock.runFor(10_000);
  await page.keyboard.press("Escape");
  await expect(page.locator(".reader")).toHaveCount(0);
  expect(await page.locator("body").getAttribute("data-dwell")).toBe(paused);
});

test("article video failures retain Open without navigating away", async ({
  page,
}) => {
  await openArticleWithVideos(page, true);
  await page
    .getByRole("button", { name: "Play Project breakdown", exact: true })
    .click();
  await expect(page.locator(".reader-video .video-failure")).toContainText(
    "Plays on YouTube only",
  );
  await expect(page.locator("body")).toHaveAttribute("data-plays", "1");
  expect(page.context().pages()).toHaveLength(1);
  await page.route("https://www.youtube.com/watch?v=dQw4w9WgXcQ", (route) =>
    route.fulfill({ body: "YouTube original" }),
  );
  const popup = page.waitForEvent("popup");
  await page
    .locator(".reader-video .video-failure")
    .getByRole("link", { name: "Open" })
    .click();
  await expect(await popup).toHaveURL(
    "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
  );
  await expect(page.locator("body")).toHaveAttribute("data-plays", "2");
});
