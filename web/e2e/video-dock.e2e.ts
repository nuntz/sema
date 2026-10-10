import { expect, type Page, test } from "@playwright/test";
import { openGrid } from "./grid-fixture";
import { stubYouTube } from "./youtube-stub";

const stage = (page: Page) => page.locator(".video-stage");
const docked = (page: Page) => page.locator('.video-stage[data-mode="dock"]');
const player = (page: Page) => page.locator(".video-stage iframe");

function behaviour(page: Page) {
  const events: string[] = [];
  page.on("request", (request) => {
    if (request.method() === "POST" && request.url().includes("/api/"))
      events.push(request.postData() || "");
  });
  return events;
}

async function dockFromPeek(page: Page) {
  await page.keyboard.press("i");
  await expect(player(page)).toHaveAttribute("data-state", "1");
  await page.keyboard.press("Escape");
  await expect(docked(page)).toBeVisible();
}

for (const width of [390, 1280]) {
  test(`a dismissed Peek keeps playing in the Dock at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await stubYouTube(page);
    const { cell } = await openGrid(page, "youtube");
    await page.keyboard.press("i");
    await expect(player(page)).toHaveAttribute("data-state", "1");
    const original = await player(page).elementHandle();
    await page.locator(".lb-scrim").click({ position: { x: 4, y: 4 } });
    await expect(docked(page)).toBeVisible();
    await expect(page.locator(".video-peek")).toHaveCount(0);
    expect(
      await player(page).evaluate((el, previous) => el === previous, original),
    ).toBe(true);
    await expect(
      page.getByRole("region", { name: "Docked video: Image article" }),
    ).toBeVisible();
    await expect(cell).not.toHaveClass(/is-read/);
    // Measured once the Peek-to-Dock transition settles.
    const gap = width >= 620 ? 20 : 12;
    await expect
      .poll(async () => {
        const box = await docked(page).boundingBox();
        return box
          ? [
              Math.round(width - box.x - box.width),
              Math.round(844 - box.y - box.height),
            ]
          : [];
      })
      .toEqual([gap, gap]);
    const box = await docked(page).boundingBox();
    expect(box?.width).toBeLessThanOrEqual(width >= 620 ? 320 : width * 0.5);
    await page.screenshot({ path: `/tmp/sema-dock-grid-${width}.png` });
    // The Dock is not an Overlay: grid keys still work beside it.
    await page.keyboard.press("i");
    await expect(page.locator(".video-peek")).toBeVisible();
    await page.screenshot({ path: `/tmp/sema-dock-expand-${width}.png` });
  });
}

test("a paused Peek closes without a Dock", async ({ page }) => {
  await stubYouTube(page, false, true);
  await openGrid(page, "youtube");
  await page.keyboard.press("i");
  await page
    .frameLocator(".video-stage iframe")
    .getByRole("button", { name: "Pause" })
    .click();
  await expect(player(page)).toHaveAttribute("data-state", "2");
  await page.keyboard.press("Escape");
  await expect(stage(page)).toHaveCount(0);
});

test("Peeking or Expanding the Docked Item reuses its player without another Play", async ({
  page,
}) => {
  await stubYouTube(page);
  const events = behaviour(page);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  const original = await player(page).elementHandle();
  await page.getByRole("button", { name: "Expand video" }).click();
  await expect(page.locator(".video-peek")).toBeFocused();
  await expect(stage(page)).toHaveAttribute("data-mode", "peek");
  await page.keyboard.press("Escape");
  await expect(docked(page)).toBeVisible();
  await page.keyboard.press("i");
  await expect(stage(page)).toHaveAttribute("data-mode", "peek");
  expect(
    await player(page).evaluate((el, previous) => el === previous, original),
  ).toBe(true);
  await expect
    .poll(() => events.join(" "), { timeout: 10_000 })
    .toContain('"clicked_through":true');
  expect(events.join(" ").match(/clicked_through/g)).toHaveLength(1);
});

test("Playing another Video Item replaces the Dock", async ({ page }) => {
  await stubYouTube(page);
  const { cell } = await openGrid(page, "youtube", false, "M", {
    second: true,
  });
  await dockFromPeek(page);
  await page
    .locator('[data-item-id="other"]')
    .getByRole("button", { name: /^Play / })
    .click();
  await expect(stage(page)).toHaveAttribute("data-mode", "peek");
  await expect(page.locator(".video-peek")).toHaveAttribute(
    "aria-label",
    "Play Other image",
  );
  await expect(player(page)).toHaveCount(1);
  await page.keyboard.press("Escape");
  await expect(
    page.getByRole("region", { name: "Docked video: Other image" }),
  ).toBeVisible();
  await expect(cell).toBeVisible();
});

test("Close ends the Dock", async ({ page }) => {
  await stubYouTube(page);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  await page.getByRole("button", { name: "Close video" }).click();
  await expect(stage(page)).toHaveCount(0);
});

test("a reader closing on a playing lead Docks it at its position, and Expand resumes the reader", async ({
  page,
}) => {
  await stubYouTube(page);
  const { cell } = await openGrid(page, "youtube");
  await cell.locator(".cell-main").click();
  await page.keyboard.press("i");
  const lead = page.locator(".reader .video-embed iframe");
  await expect(lead).toHaveAttribute("data-state", "1");
  await page.keyboard.press("Escape");
  await expect(page.locator(".reader")).toHaveCount(0);
  await expect(docked(page)).toBeVisible();
  await expect(player(page)).toHaveAttribute("data-seconds", "87");
  await page.getByRole("button", { name: "Expand video" }).click();
  await expect(stage(page)).toHaveCount(0);
  await expect(page.locator(".reader iframe")).toHaveAttribute(
    "data-seconds",
    "87",
  );
});

test("a reader moving to the next Item Docks its playing lead", async ({
  page,
}) => {
  await stubYouTube(page);
  await openGrid(page, "youtube", false, "M", { second: "article" });
  await page.locator('[data-item-id="peek"] .cell-main').click();
  await page.keyboard.press("i");
  await expect(page.locator(".reader iframe")).toHaveAttribute(
    "data-state",
    "1",
  );
  await page.keyboard.press("n");
  await expect(page.locator(".reader")).toHaveAttribute(
    "aria-label",
    "Other article",
  );
  await expect(
    page.getByRole("region", { name: "Docked video: Image article" }),
  ).toBeVisible();
});

test("Playing in the reader stops the Dock", async ({ page }) => {
  await stubYouTube(page);
  await openGrid(page, "youtube", false, "M", { second: true });
  await dockFromPeek(page);
  await page.locator('[data-item-id="other"] .cell-main').click();
  await expect(page.locator(".reader")).toHaveAttribute(
    "aria-label",
    "Other image",
  );
  await expect(docked(page)).toBeVisible();
  await page.keyboard.press("i");
  await expect(page.locator(".reader iframe")).toHaveCount(1);
  await expect(stage(page)).toHaveCount(0);
});

test("on a phone the Dock rides above the reader toolbar and Expands into the reader", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await stubYouTube(page);
  await openGrid(page, "youtube", false, "M", { second: "article" });
  await dockFromPeek(page);
  await page.locator('[data-item-id="other"] .cell-main').click();
  await expect(page.locator(".reader")).toHaveAttribute(
    "aria-label",
    "Other article",
  );
  const toolbar = page.locator(".reader-bottom-actions");
  await expect(toolbar).toBeVisible();
  await expect
    .poll(async () => {
      const dock = await docked(page).boundingBox();
      const bar = await toolbar.boundingBox();
      return dock && bar ? Math.round(bar.y - (dock.y + dock.height)) : -1;
    })
    .toBe(12);
  await page.screenshot({ path: "/tmp/sema-dock-reader-390.png" });
  await page.getByRole("button", { name: "Expand video" }).click();
  await expect(page.locator(".reader")).toHaveAttribute(
    "aria-label",
    "Image article",
  );
  await expect(page.locator(".reader iframe")).toHaveAttribute(
    "data-state",
    "1",
  );
  await expect(stage(page)).toHaveCount(0);
});

test("other Overlays cover the Dock, leaving it dimmed and inert", async ({
  page,
}) => {
  await stubYouTube(page);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  await page.locator('[data-item-id="peek"] .cell-main').focus();
  await page.keyboard.press("Shift+F10");
  await expect(page.locator(".action-sheet-layer")).toBeVisible();
  await expect(docked(page)).toHaveClass(/is-covered/);
  expect(await docked(page).evaluate((el) => (el as HTMLElement).inert)).toBe(
    true,
  );
  await page.keyboard.press("Escape");
  await expect(docked(page)).not.toHaveClass(/is-covered/);
});

test("Docked playback earns dwell from zero after a Peek", async ({ page }) => {
  await page.clock.install();
  await stubYouTube(page);
  const events = behaviour(page);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  await page.clock.runFor(31_000);
  await page.clock.runFor(6_000);
  await expect
    .poll(() => events.join(" "), { timeout: 10_000 })
    .toMatch(/"dwell_ms":3\d{4}/);
});

test("leaving the Dock after using YouTube's controls returns the keyboard", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 844 });
  await stubYouTube(page, false, true);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  await player(page)
    .contentFrame()
    .getByRole("button", { name: "Mute" })
    .click();
  await page.mouse.move(10, 400);
  await expect
    .poll(() =>
      page.evaluate(() => document.activeElement?.tagName === "IFRAME"),
    )
    .toBe(false);
  await page.keyboard.press("i");
  await expect(stage(page)).toHaveAttribute("data-mode", "peek");
});

test("Enter on the Dock's buttons acts on the Dock, not the grid", async ({
  page,
}) => {
  await stubYouTube(page);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  await page.getByRole("button", { name: "Expand video" }).focus();
  await page.keyboard.press("Enter");
  await expect(stage(page)).toHaveAttribute("data-mode", "peek");
  await page.keyboard.press("Escape");
  await expect(docked(page)).toBeVisible();
  await page.getByRole("button", { name: "Close video" }).focus();
  await page.keyboard.press("Enter");
  await expect(stage(page)).toHaveCount(0);
  await expect(page.locator(".reader")).toHaveCount(0);
});

test("the Peek's modal dialog contains its player", async ({ page }) => {
  await stubYouTube(page);
  await openGrid(page, "youtube");
  await page.keyboard.press("i");
  const dialog = page.getByRole("dialog", { name: "Play Image article" });
  await expect(dialog).toHaveAttribute("aria-modal", "true");
  await expect(dialog.locator("iframe")).toHaveCount(1);
  await expect(player(page)).toHaveAttribute("data-state", "1");
  await page.keyboard.press("Escape");
  await expect(docked(page)).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
});

test("signing out reports the Dock's dwell", async ({ page }) => {
  await page.clock.install();
  await stubYouTube(page);
  const events = behaviour(page);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  await page.clock.runFor(20_000);
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "sign out" }).click();
  await expect
    .poll(() => events.join(" "), { timeout: 10_000 })
    .toMatch(/"dwell_ms":2\d{4}/);
});

test("the Dock keeps playing, uncovered, through Settings & Feeds", async ({
  page,
}) => {
  await stubYouTube(page);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  const original = await player(page).elementHandle();
  await page.getByRole("button", { name: "Settings" }).click();
  await expect(page.getByRole("button", { name: "sign out" })).toBeVisible();
  await expect(docked(page)).toBeVisible();
  await expect(docked(page)).not.toHaveClass(/is-covered/);
  expect(await docked(page).evaluate((el) => (el as HTMLElement).inert)).toBe(
    false,
  );
  expect(
    await player(page).evaluate((el, previous) => el === previous, original),
  ).toBe(true);
  await page.keyboard.press("Escape");
  await expect(page.locator('[data-item-id="peek"]')).toBeVisible();
  expect(
    await player(page).evaluate((el, previous) => el === previous, original),
  ).toBe(true);
  await expect(player(page)).toHaveAttribute("data-state", "1");
});

test("Expand from Settings & Feeds resumes a reader-born Dock in its reader", async ({
  page,
}) => {
  await stubYouTube(page);
  const { cell } = await openGrid(page, "youtube");
  await cell.locator(".cell-main").click();
  await page.keyboard.press("i");
  await expect(page.locator(".reader iframe")).toHaveAttribute(
    "data-state",
    "1",
  );
  await page.keyboard.press("Escape");
  await expect(docked(page)).toBeVisible();
  await page.getByRole("button", { name: "Settings" }).click();
  await expect(page.getByRole("button", { name: "sign out" })).toBeVisible();
  await page.getByRole("button", { name: "Expand video" }).click();
  await expect(page.getByRole("button", { name: "sign out" })).toHaveCount(0);
  await expect(page.locator(".reader iframe")).toHaveAttribute(
    "data-seconds",
    "87",
  );
  await expect(stage(page)).toHaveCount(0);
  await page.goBack();
  await expect(page.locator(".reader")).toHaveCount(0);
  await expect(cell).toBeVisible();
  await expect(page.getByRole("button", { name: "sign out" })).toHaveCount(0);
});

test("Expand from Settings & Feeds Peeks over the grid, and Flip opens the reader", async ({
  page,
}) => {
  await stubYouTube(page);
  await openGrid(page, "youtube");
  await dockFromPeek(page);
  const original = await player(page).elementHandle();
  await page.getByRole("button", { name: "Settings" }).click();
  await expect(page.getByRole("button", { name: "sign out" })).toBeVisible();
  await page.getByRole("button", { name: "Expand video" }).click();
  await expect(page.locator(".video-peek")).toBeFocused();
  await expect(page.getByRole("button", { name: "sign out" })).toHaveCount(0);
  expect(
    await player(page).evaluate((el, previous) => el === previous, original),
  ).toBe(true);
  await page.keyboard.press("o");
  await expect(page.locator(".reader iframe")).toHaveAttribute(
    "data-seconds",
    "87",
  );
  await expect(stage(page)).toHaveCount(0);
});
