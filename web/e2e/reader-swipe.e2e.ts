import { expect, type Page, test } from "@playwright/test";

async function touch(page: Page, type: string, x: number) {
  return page.locator(".reader-scroll").evaluate(
    (target, { type, x }) => {
      const point = new Touch({
        identifier: 1,
        target,
        clientX: x,
        clientY: 300,
      });
      return target.dispatchEvent(
        new TouchEvent(type, {
          bubbles: true,
          cancelable: true,
          touches: type === "touchend" || type === "touchcancel" ? [] : [point],
          changedTouches: [point],
        }),
      );
    },
    { type, x },
  );
}

for (const standalone of [false, true]) {
  test(`reader leaves edge back gestures to the browser (standalone=${standalone})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.addInitScript((standalone) => {
      Object.defineProperty(navigator, "standalone", { value: standalone });
    }, standalone);
    await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
    const reader = page.locator(".reader");
    await expect(reader).toBeVisible();
    await touch(page, "touchstart", 10);
    // A native back swipe moves the whole webview. Moving the reader too
    // exposes its live grid beside the browser's snapshot of the same grid.
    const allowed = await touch(page, "touchmove", 170);
    await expect(reader).toHaveCSS("transform", "matrix(1, 0, 0, 1, 0, 0)");
    expect(allowed).toBe(true);
    await touch(page, "touchend", 170);
    await expect(reader).toBeVisible();
    await page.goBack();
    await expect(reader).toHaveCount(0);
  });
}

test("reader still follows interior swipes, cancels, and closes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/e2e/header-fixture.html?view=reader&lightbox=1");
  const reader = page.locator(".reader");
  await expect(reader).toBeVisible();
  await touch(page, "touchstart", 60);
  expect(await touch(page, "touchmove", 180)).toBe(false);
  await expect(reader).toHaveCSS("transform", "matrix(1, 0, 0, 1, 120, 0)");
  await touch(page, "touchcancel", 180);
  await expect(reader).toHaveCSS("transform", "matrix(1, 0, 0, 1, 0, 0)");
  await touch(page, "touchstart", 60);
  await touch(page, "touchmove", 260);
  await touch(page, "touchend", 260);
  await expect(reader).toHaveCount(0);
});
