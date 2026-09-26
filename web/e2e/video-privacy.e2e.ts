import { expect, test } from "@playwright/test";
import { stubYouTube } from "./youtube-stub";

for (const kind of ["video", "article"]) {
  test(`${kind} makes no Google requests before Play and embeds only on the privacy host`, async ({
    page,
  }) => {
    await stubYouTube(page);
    const requests: URL[] = [];
    page.on("request", (request) => requests.push(new URL(request.url())));
    const google = (url: URL) =>
      ["youtube.com", "ytimg.com", "googleapis.com", "google.com"].some(
        (host) => url.hostname === host || url.hostname.endsWith(`.${host}`),
      );
    await page.route(
      (url) => google(url),
      (route) => route.abort(),
    );
    if (kind === "article")
      await page.route("**/e2e/reader-body.html", (route) =>
        route.fulfill({
          contentType: "text/html",
          body: `<p>Article with legacy thumbnails.</p><a class="media-card" href="https://www.youtube.com/watch?v=dQw4w9WgXcQ" title="Project"><span class="media-card-thumbnail"><img loading="eager" src="https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg"></span></a><a class="media-card" href="https://vimeo.com/123"><img src="https://i.ytimg.com/remote.jpg"></a><a class="media-card" href="https://vimeo.com/456"><img src="/media/e2e/reader-media/0.svg" srcset="https://i.ytimg.com/srcset.jpg 1x"></a>`,
        }),
      );
    await page.goto(
      `/e2e/header-fixture.html?view=reader&media=${kind}&lightbox=1`,
    );
    await expect(page.locator(".video-media-card")).toBeVisible();
    if (kind === "article") {
      await expect(page.locator(".reader-video img")).toHaveCount(0);
      await expect(page.locator(".media-card img")).toHaveCount(1);
      await expect(page.locator(".media-card img")).toHaveAttribute(
        "referrerpolicy",
        "no-referrer",
      );
      await expect(page.locator(".media-card img")).not.toHaveAttribute(
        "srcset",
      );
    }
    expect(requests.filter(google)).toEqual([]);
    expect(
      requests.filter((url) => url.hostname.endsWith("youtube-nocookie.com")),
    ).toEqual([]);
    if (kind === "video") await page.keyboard.press("i");
    else
      await page
        .getByRole("button", { name: "Play Project", exact: true })
        .click();
    await expect(page.locator(".video-embed iframe")).toHaveAttribute(
      "data-state",
      "1",
    );
    expect(requests.filter(google)).toEqual([]);
    const remote = requests.filter(
      (url) => url.origin !== new URL(page.url()).origin,
    );
    expect(remote.length).toBeGreaterThan(0);
    expect(
      remote.every((url) => url.origin === "https://www.youtube-nocookie.com"),
    ).toBe(true);
    await expect(page.locator('script[src*="youtube"]')).toHaveCount(0);
  });
}
