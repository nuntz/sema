import { expect, type Page, test } from "@playwright/test";

async function openFixture(
  page: Page,
  view: "grid" | "reader",
  font: "loaded" | "blocked" = "blocked",
) {
  await page.goto(`/e2e/header-fixture.html?view=${view}&font=${font}`);
  await page.locator(".app-header").waitFor();
  await page.evaluate(() => document.fonts.ready);
}

async function bandScreenshot(page: Page) {
  const header = await page.locator(".app-header").boundingBox();
  if (!header) throw new Error("Header has no bounding box");
  return page.screenshot({
    clip: {
      x: header.x,
      y: header.y + header.height - 2,
      width: header.width,
      height: 2,
    },
  });
}

test("grid and reader share pixel-identical brand and band chrome", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1544, height: 900 });
  await openFixture(page, "grid", "loaded");
  const gridBrand = await page.locator(".app-header__brand").screenshot();
  const gridBand = await bandScreenshot(page);

  await openFixture(page, "reader", "loaded");
  const readerBrand = await page.locator(".app-header__brand").screenshot();
  const readerBand = await bandScreenshot(page);

  expect(readerBrand).toEqual(gridBrand);
  expect(readerBand).toEqual(gridBand);
});

test("both desktop views use the same 56px mono control geometry", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1544, height: 900 });
  for (const view of ["grid", "reader"] as const) {
    await openFixture(page, view, "loaded");
    const geometry = await page.evaluate(() => {
      const header = document.querySelector(".app-header");
      const mark = document.querySelector(".app-header__brand svg");
      if (!header || !mark)
        throw new Error("Shared header geometry is missing");
      const controls = [
        ...document.querySelectorAll(
          ".app-header :is(.segmented__item, .chrome-btn, .chrome-icon, .reader-back)",
        ),
      ].filter((element) => element.getClientRects().length > 0);
      const sans = [...header.querySelectorAll("*")].filter((element) =>
        getComputedStyle(element).fontFamily.includes("Instrument Sans"),
      );
      return {
        header: header.getBoundingClientRect(),
        mark: mark.getBoundingClientRect(),
        bandHeight: Number.parseFloat(
          getComputedStyle(header, "::after").height,
        ),
        controls: controls.map((element) => element.getBoundingClientRect()),
        sansCount: sans.length,
      };
    });

    expect(geometry.header.height).toBe(56);
    expect(geometry.mark.x).toBe(20);
    expect(geometry.mark.y).toBe(18);
    expect(geometry.mark.width).toBe(20);
    expect(geometry.mark.height).toBe(20);
    expect(geometry.bandHeight).toBe(2);
    expect(geometry.controls.every((control) => control.height === 30)).toBe(
      true,
    );
    expect(geometry.controls.every((control) => control.y === 13)).toBe(true);
    expect(geometry.sansCount).toBe(1);
  }
});

for (const font of ["loaded", "blocked"] as const) {
  for (const width of [1280, 1366, 1544, 1920]) {
    test(`reader slot aligns at ${width}px with the webfont ${font}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await openFixture(page, "reader", font);

      const geometry = await page.evaluate(() => {
        const rect = (selector: string) => {
          const element = document.querySelector(selector);
          if (!element) throw new Error(`Missing ${selector}`);
          return element.getBoundingClientRect();
        };
        return {
          article: rect(".article h1").left,
          crumb: rect(".reader-crumb").left,
          title: rect(".reader-title").left,
          favicon: rect(".reader-favicon").left,
        };
      });

      expect(Math.abs(geometry.crumb - geometry.article)).toBeLessThanOrEqual(
        1,
      );
      expect(Math.abs(geometry.title - geometry.article)).toBeLessThanOrEqual(
        1,
      );
      expect(
        Math.abs(geometry.article - geometry.favicon - 29),
      ).toBeLessThanOrEqual(1);
    });
  }
}

test("segmented pills preserve v1 gaps while items own the hit target", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1544, height: 900 });
  await openFixture(page, "grid");

  const tracks = await page.locator(".segmented").evaluateAll((elements) =>
    elements.map((track) => {
      const trackRect = track.getBoundingClientRect();
      const items = [...track.querySelectorAll(".segmented__item")];
      const pills = items.map((item) => {
        const itemRect = item.getBoundingClientRect();
        const pill = getComputedStyle(item, "::before");
        return {
          left: itemRect.left + Number.parseFloat(pill.left),
          right: itemRect.right - Number.parseFloat(pill.right),
          height: itemRect.height,
        };
      });
      return {
        outerStart: pills[0].left - trackRect.left,
        interior: pills[1].left - pills[0].right,
        outerEnd: trackRect.right - pills[pills.length - 1].right,
        heights: pills.map((pill) => pill.height),
      };
    }),
  );

  for (const track of tracks) {
    expect(track.outerStart).toBe(3);
    expect(track.interior).toBe(2);
    expect(track.outerEnd).toBe(3);
    expect(track.heights.every((height) => height >= 30)).toBe(true);
  }

  await page.setViewportSize({ width: 390, height: 780 });
  const phoneHeights = await page
    .locator(".segmented__item")
    .evaluateAll((items) =>
      items.map((item) => item.getBoundingClientRect().height),
    );
  expect(phoneHeights.every((height) => height >= 44)).toBe(true);
});

test("reader title overlays the crumb and crossfades with hysteresis", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1544, height: 600 });
  await openFixture(page, "reader");
  await page.locator("#reader-last-line").waitFor();

  const before = await page.locator(".reader-crumb").boundingBox();
  await page.locator(".reader-title").evaluate((title) => title.remove());
  const withoutTitle = await page.locator(".reader-crumb").boundingBox();
  expect(withoutTitle?.width).toBe(before?.width);

  await page.reload();
  await page.locator("#reader-last-line").waitFor();
  const scroll = page.locator(".reader-scroll");
  await scroll.evaluate((element) => {
    element.scrollTop = 181;
  });
  await expect(page.locator(".app-header")).toHaveAttribute(
    "data-scrolled",
    "",
  );
  await expect(page.locator(".reader-crumb")).toHaveAttribute(
    "aria-hidden",
    "true",
  );
  await expect(page.locator(".reader-title")).toHaveAttribute(
    "aria-hidden",
    "false",
  );

  await scroll.evaluate((element) => {
    element.scrollTop = 170;
  });
  await expect(page.locator(".app-header")).toHaveAttribute(
    "data-scrolled",
    "",
  );
  await scroll.evaluate((element) => {
    element.scrollTop = 159;
  });
  await expect(page.locator(".app-header")).not.toHaveAttribute(
    "data-scrolled",
    "",
  );
});

test("reader page keys scroll its article viewport", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 420 });
  await openFixture(page, "reader");
  await page.locator("#reader-last-line").waitFor();

  const scroll = page.locator(".reader-scroll");
  const scrollTop = () => scroll.evaluate((element) => element.scrollTop);

  await page.keyboard.press("Space");
  await expect.poll(scrollTop).toBeGreaterThan(0);
  const afterSpace = await scrollTop();

  await page.keyboard.press("PageDown");
  await expect.poll(scrollTop).toBeGreaterThan(afterSpace);
  const afterPageDown = await scrollTop();

  await page.keyboard.press("PageUp");
  await expect.poll(scrollTop).toBeLessThan(afterPageDown);
});

test("reader judgments preserve article scroll and do not refetch its body", async ({
  page,
}) => {
  let bodyRequests = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/e2e/reader-body.html") {
      bodyRequests++;
    }
  });

  await openFixture(page, "reader");
  await page.locator("#reader-last-line").waitFor();

  const scroll = page.locator(".reader-scroll");
  const actions = page.locator(".chrome-group--judge button");
  for (const [index, label] of ["boost", "bury", "keep"].entries()) {
    const before = await scroll.evaluate((element) => {
      element.scrollTop = Math.min(
        300,
        element.scrollHeight - element.clientHeight,
      );
      return element.scrollTop;
    });
    expect(before).toBeGreaterThan(0);

    const action = actions.nth(index);
    await expect(action).toContainText(label);
    await action.click();
    await expect(action).toHaveAttribute("aria-pressed", "true");
    await expect
      .poll(() => scroll.evaluate((element) => element.scrollTop))
      .toBe(before);
  }

  expect(bodyRequests).toBe(1);
});

test("responsive chrome visibility, semantics, and overflow stay valid", async ({
  page,
}) => {
  for (const [view, width] of [
    ["grid", 1544],
    ["reader", 1544],
    ["grid", 620],
    ["reader", 620],
    ["grid", 390],
    ["reader", 390],
  ] as const) {
    await page.setViewportSize({ width, height: 780 });
    await openFixture(page, view);
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBeLessThanOrEqual(width);
    expect(await page.locator(".app-header [hidden]").count()).toBe(0);
    expect(await page.locator('.app-header [role="tab"]').count()).toBe(0);
    expect(await page.locator('.app-header [role="tablist"]').count()).toBe(0);
    expect(await page.locator(".app-header [aria-selected]").count()).toBe(0);
    expect(await page.locator(".chrome-group .chrome-divider").count()).toBe(0);

    const geometry = await page.evaluate(() => {
      const header = document.querySelector(".app-header");
      const mark = document.querySelector(".app-header__brand svg");
      if (!header || !mark)
        throw new Error("Shared header geometry is missing");
      const controls = [...header.querySelectorAll("button, a")].filter(
        (element) => element.getClientRects().length > 0,
      );
      return {
        header: header.getBoundingClientRect(),
        mark: mark.getBoundingClientRect(),
        controls: controls.map((element) => element.getBoundingClientRect()),
      };
    });
    expect(geometry.header.height).toBe(56);
    if (width > 430) {
      expect(geometry.mark.x).toBe(20);
      expect(geometry.mark.y).toBe(18);
    } else {
      expect(geometry.mark.width).toBe(0);
      expect(geometry.mark.height).toBe(0);
    }
    expect(
      geometry.controls.every((control) =>
        width <= 430 ? control.height >= 44 : control.height >= 30,
      ),
    ).toBe(true);
  }

  await page.setViewportSize({ width: 1544, height: 780 });
  await openFixture(page, "grid");
  await expect(page.locator(".filter-button")).toBeHidden();
  await openFixture(page, "reader");
  await expect(page.locator(".chrome-overflow")).toBeHidden();

  await page.setViewportSize({ width: 619, height: 780 });
  await openFixture(page, "reader");
  await expect(page.locator(".chrome-overflow")).toBeVisible();

  await page.setViewportSize({ width: 900, height: 780 });
  await openFixture(page, "reader");
  await expect(
    page.locator(".chrome-group--secondary .chrome-btn__label").first(),
  ).toBeHidden();
  await expect(
    page.locator(".chrome-group--judge .chrome-btn__label").first(),
  ).toBeVisible();

  await page.setViewportSize({ width: 620, height: 780 });
  await openFixture(page, "reader");
  await expect(
    page.locator(".chrome-group--judge .chrome-btn__label").first(),
  ).toBeHidden();
  await expect(
    page.locator(".chrome-group--judge .chrome-btn__label").last(),
  ).toBeVisible();
  await expect(page.locator(".reader-slot__text")).toBeHidden();

  await page.setViewportSize({ width: 390, height: 780 });
  await openFixture(page, "grid");
  await expect(page.locator(".filter-button")).toBeVisible();
  await expect(page.locator(".header-segments")).toBeHidden();
});

test("phone reader exposes five 44px actions and clears the final line", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  await openFixture(page, "reader");
  await page.locator("#reader-last-line").waitFor();

  const actions = page.locator(".reader-bottom-actions button");
  await expect(actions).toHaveCount(5);
  const heights = await actions.evaluateAll((buttons) =>
    buttons.map((button) => button.getBoundingClientRect().height),
  );
  expect(heights.every((height) => height >= 44)).toBe(true);
  await expect(page.locator(".reader-bottom-actions")).toContainText("next");

  await page.locator(".reader-scroll").evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  const clearance = await page.evaluate(() => {
    const lastLine = document.querySelector("#reader-last-line");
    const bar = document.querySelector(".reader-bottom-actions");
    if (!lastLine || !bar)
      throw new Error("Phone reader fixture is incomplete");
    return (
      bar.getBoundingClientRect().top - lastLine.getBoundingClientRect().bottom
    );
  });
  expect(clearance).toBeGreaterThanOrEqual(0);
});
