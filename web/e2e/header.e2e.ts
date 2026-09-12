import { expect, type Page, test } from "@playwright/test";

async function openFixture(
  page: Page,
  view: "grid" | "reader",
  font: "loaded" | "blocked" = "blocked",
) {
  await page.goto(`/e2e/header-fixture.html?view=${view}&font=${font}`);
  await page.locator(".app-header").waitFor();
  await page.evaluate(() => document.fonts.ready);
  if (view === "reader")
    await page.locator(".reader").evaluate(async (reader) => {
      await Promise.all(
        reader.getAnimations().map((animation) => animation.finished),
      );
    });
}

async function scrollReaderPastHeadline(page: Page) {
  await page.locator("#reader-last-line").waitFor();
  await page.locator(".reader-scroll").evaluate((element) => {
    element.scrollTop = 181;
  });
  await expect(page.locator(".app-header")).toHaveAttribute(
    "data-scrolled",
    "",
  );
}

test("grid and reader share brand chrome and a 2px bottom band", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1544, height: 900 });
  await openFixture(page, "grid", "loaded");
  const gridBrand = await page.locator(".app-header__brand").screenshot();
  const gridBandHeight = await page
    .locator(".app-header")
    .evaluate((header) =>
      Number.parseFloat(getComputedStyle(header, "::after").height),
    );

  await openFixture(page, "reader", "loaded");
  const readerBrand = await page.locator(".app-header__brand").screenshot();
  const readerBandHeight = await page
    .locator(".app-header")
    .evaluate((header) =>
      Number.parseFloat(getComputedStyle(header, "::after").height),
    );

  expect(readerBrand).toEqual(gridBrand);
  expect(gridBandHeight).toBe(2);
  expect(readerBandHeight).toBe(gridBandHeight);
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
    expect(geometry.sansCount).toBe(view === "reader" ? 2 : 1);
  }
});

test("responsive type scales preserve the UI floor and reader targets", async ({
  page,
}) => {
  for (const [width, expected] of [
    [
      1544,
      {
        tokens: ["0.8125rem", "0.9375rem", "1.0625rem", "0.8125rem"],
        chrome: "13px",
        wordmark: "17px",
        article: "21px",
        title: "44px",
      },
    ],
    [
      390,
      {
        tokens: ["0.875rem", "1rem", "1.125rem", "0.875rem"],
        chrome: "14px",
        wordmark: "18px",
        article: "19px",
        title: "30px",
      },
    ],
  ] as const) {
    await page.setViewportSize({ width, height: 900 });
    await openFixture(page, "grid", "loaded");
    const gridType = await page.evaluate(() => {
      const root = getComputedStyle(document.documentElement);
      const fontSize = (selector: string) => {
        const element = document.querySelector(selector);
        if (!element) throw new Error(`Missing ${selector}`);
        return getComputedStyle(element).fontSize;
      };
      return {
        tokens: ["--type-xs", "--type-sm", "--type-md", "--type-chip"].map(
          (token) => root.getPropertyValue(token).trim(),
        ),
        chrome: fontSize(".segmented__item"),
        wordmark: fontSize(".app-mark > span"),
      };
    });
    expect(gridType).toEqual({
      tokens: expected.tokens,
      chrome: expected.chrome,
      wordmark: expected.wordmark,
    });

    await openFixture(page, "reader", "loaded");
    await page.locator("#reader-last-line").waitFor();
    const readerType = await page.evaluate(() => ({
      article: getComputedStyle(
        document.querySelector(".article-body") as Element,
      ).fontSize,
      title: getComputedStyle(document.querySelector(".article h1") as Element)
        .fontSize,
    }));
    expect(readerType).toEqual({
      article: expected.article,
      title: expected.title,
    });
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

test("desktop reader progress follows the article measure inside the band", async ({
  page,
}) => {
  for (const width of [1000, 1280, 1524]) {
    await page.setViewportSize({ width, height: 900 });
    await openFixture(page, "reader", "loaded");
    await scrollReaderPastHeadline(page);

    const geometry = await page.evaluate(() => {
      const article = document.querySelector(".article h1");
      const header = document.querySelector(".app-header");
      const progress = document.querySelector(".read-progress");
      if (!article || !header || !progress)
        throw new Error("Reader progress geometry is missing");
      return {
        article: article.getBoundingClientRect(),
        bandHeight: Number.parseFloat(
          getComputedStyle(header, "::after").height,
        ),
        header: header.getBoundingClientRect(),
        progress: progress.getBoundingClientRect(),
      };
    });

    expect(
      Math.abs(geometry.progress.left - geometry.article.left),
    ).toBeLessThanOrEqual(1);
    if (width === 1000) {
      expect(
        Math.abs(geometry.progress.width - geometry.article.width),
      ).toBeLessThanOrEqual(1);
    } else {
      expect(Math.abs(geometry.progress.width - 640)).toBeLessThanOrEqual(1);
    }
    expect(geometry.progress.height).toBe(2);
    expect(geometry.bandHeight).toBe(2);
    expect(geometry.progress.bottom).toBe(geometry.header.bottom);
  }
});

test("scrolled reader title uses sans without overflowing desktop chrome", async ({
  page,
}) => {
  for (const width of [1200, 1280, 1400, 1524]) {
    await page.setViewportSize({ width, height: 900 });
    await openFixture(page, "reader", "loaded");
    await scrollReaderPastHeadline(page);

    const layout = await page.evaluate(() => {
      const header = document.querySelector(".app-header");
      const title = document.querySelector(".reader-title");
      if (!header || !title) throw new Error("Reader title is missing");
      return {
        clientWidth: header.clientWidth,
        fontFamily: getComputedStyle(title).fontFamily,
        scrollWidth: header.scrollWidth,
        titleClientWidth: title.clientWidth,
        titleScrollWidth: title.scrollWidth,
      };
    });

    expect(layout.fontFamily).toContain("Instrument Sans");
    expect(layout.scrollWidth).toBe(layout.clientWidth);
    if (width === 1524) {
      expect(layout.titleScrollWidth).toBeLessThanOrEqual(
        layout.titleClientWidth,
      );
    }
  }
});

test("scrolled reader actions adopt their desktop posture", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openFixture(page, "reader", "loaded");
  await scrollReaderPastHeadline(page);

  const quietButtons = page.locator(".app-header--reader .chrome-btn--quiet");
  await expect(quietButtons).toHaveCount(5);
  const expectedColors = await page.evaluate(() => {
    const header = document.querySelector(".app-header");
    if (!header) throw new Error("Reader header is missing");
    const probe = document.createElement("i");
    probe.style.backgroundColor = "var(--surface-chrome-raised)";
    probe.style.color = "var(--chrome-fg-on)";
    document.body.append(probe);
    const colors = {
      header: getComputedStyle(header).backgroundColor,
      on: getComputedStyle(probe).color,
      raised: getComputedStyle(probe).backgroundColor,
    };
    probe.remove();
    return colors;
  });

  await expect
    .poll(() =>
      quietButtons.evaluateAll((buttons) =>
        buttons.map((button) => getComputedStyle(button).backgroundColor),
      ),
    )
    .toEqual(Array(5).fill(expectedColors.header));
  expect(
    await quietButtons
      .locator(".chrome-btn__label")
      .evaluateAll((labels) =>
        labels.every((label) => getComputedStyle(label).display === "none"),
      ),
  ).toBe(true);

  const keep = page.locator(".chrome-btn--hold");
  await expect
    .poll(() =>
      keep.evaluate((button) => getComputedStyle(button).backgroundColor),
    )
    .toBe(expectedColors.raised);
  await expect(
    page.locator(".chrome-btn--prev .chrome-btn__label"),
  ).toBeVisible();

  await keep.click();
  await expect(keep).toHaveAttribute("aria-pressed", "true");
  await expect
    .poll(() => keep.evaluate((button) => getComputedStyle(button).color))
    .toBe(expectedColors.on);

  await page.setViewportSize({ width: 1000, height: 900 });
  await openFixture(page, "reader", "loaded");
  await scrollReaderPastHeadline(page);
  await expect(
    page.locator(".chrome-btn--prev .chrome-btn__label"),
  ).toBeHidden();
});

test("phone reader progress remains full width and scroll-gated", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  await openFixture(page, "reader", "loaded");
  const progress = page.locator(".read-progress");
  const geometry = () =>
    progress.evaluate((element) => {
      const rect = element.getBoundingClientRect();
      return {
        background: getComputedStyle(element).backgroundColor,
        left: rect.left,
        opacity: getComputedStyle(element).opacity,
        transform: getComputedStyle(element).transform,
        width: rect.width,
      };
    });

  expect(await geometry()).toEqual({
    background: "rgba(0, 0, 0, 0)",
    left: 0,
    opacity: "0",
    transform: "none",
    width: 390,
  });

  await scrollReaderPastHeadline(page);
  expect(await geometry()).toEqual({
    background: "rgba(0, 0, 0, 0)",
    left: 0,
    opacity: "1",
    transform: "none",
    width: 390,
  });
});

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

for (const reducedMotion of ["no-preference", "reduce"] as const) {
  test(`reader page keys scroll its article viewport with ${reducedMotion} motion`, async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion });
    await page.setViewportSize({ width: 1280, height: 420 });
    await openFixture(page, "reader");
    await page.locator("#reader-last-line").waitFor();

    const scroll = page.locator(".reader-scroll");
    const scrollTop = () => scroll.evaluate((element) => element.scrollTop);
    const { height, maxTop } = await scroll.evaluate((element) => {
      const height = element.clientHeight;
      const maxTop = element.scrollHeight - height;
      element.addEventListener("scroll", () => {
        if (
          element.scrollTop > 0 &&
          element.scrollTop < Math.min(height, maxTop)
        )
          element.setAttribute("data-intermediate-scroll", "true");
      });
      return { height, maxTop };
    });
    expect(maxTop).toBeGreaterThan(0);

    await page.keyboard.press("Space");
    await expect.poll(scrollTop).toBe(Math.min(height, maxTop));
    expect(await scroll.getAttribute("data-intermediate-scroll")).toBe(
      reducedMotion === "reduce" ? null : "true",
    );
    const afterSpace = await scrollTop();

    await page.keyboard.press("PageDown");
    await expect.poll(scrollTop).toBe(Math.min(afterSpace + height, maxTop));
    const afterPageDown = await scrollTop();

    await page.keyboard.press("PageUp");
    await expect.poll(scrollTop).toBe(Math.max(0, afterPageDown - height));
    const afterPageUp = await scrollTop();

    await page.keyboard.press("Shift+Space");
    await expect.poll(scrollTop).toBe(Math.max(0, afterPageUp - height));
  });
}

for (const width of [1280, 390]) {
  test(`reader page keys stop at the content boundaries at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 420 });
    await openFixture(page, "reader");
    await page.locator("#reader-last-line").waitFor();
    const scroll = page.locator(".reader-scroll");
    const maxTop = await scroll.evaluate((element) => {
      const maxTop = element.scrollHeight - element.clientHeight;
      element.scrollTop = maxTop - 40;
      for (const method of ["scrollBy", "scrollTo"] as const) {
        element[method] = new Proxy(element[method], {
          apply(target, thisArg, args) {
            const top =
              typeof args[0] === "number" ? args[1] : (args[0]?.top ?? 0);
            const destination =
              method === "scrollBy" ? element.scrollTop + top : top;
            element.setAttribute(
              "data-scroll-destination",
              String(destination),
            );
            element.setAttribute(
              "data-scroll-requests",
              String(Number(element.getAttribute("data-scroll-requests")) + 1),
            );
            return Reflect.apply(target, thisArg, args);
          },
        });
      }
      return maxTop;
    });
    await page.keyboard.press("Space");
    await expect
      .poll(() => scroll.evaluate((element) => element.scrollTop))
      .toBe(maxTop);
    await expect(scroll).toHaveAttribute(
      "data-scroll-destination",
      String(maxTop),
    );
    for (const key of ["Space", "Space", "PageDown"])
      await page.keyboard.press(key);
    await expect(scroll).toHaveAttribute("data-scroll-requests", "1");
    expect(await scroll.evaluate((element) => element.scrollTop)).toBe(maxTop);
    await expect(page.locator("#reader-last-line")).toBeInViewport();
    await expect(scroll).toHaveCSS("overscroll-behavior-y", "none");

    await scroll.evaluate((element) => {
      element.scrollTop = 40;
    });
    await page.keyboard.press("Shift+Space");
    await expect
      .poll(() => scroll.evaluate((element) => element.scrollTop))
      .toBe(0);
    await expect(scroll).toHaveAttribute("data-scroll-destination", "0");
    for (const key of ["Shift+Space", "PageUp"]) await page.keyboard.press(key);
    await expect(scroll).toHaveAttribute("data-scroll-requests", "2");
  });
}

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

    const anchorTop = await page
      .locator("#reader-last-line")
      .evaluate((element) => element.getBoundingClientRect().top);
    const action = actions.nth(index);
    await expect(action).toContainText(label);
    await action.click();
    await expect(action).toHaveAttribute("aria-pressed", "true");
    await expect
      .poll(() =>
        page
          .locator("#reader-last-line")
          .evaluate(
            (element, originalTop) =>
              Math.abs(element.getBoundingClientRect().top - originalTop),
            anchorTop,
          ),
      )
      .toBeLessThan(1);
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
      const controls = [
        ...header.querySelectorAll(
          ".segmented__item, .chrome-btn, .chrome-icon, .reader-back",
        ),
      ].filter((element) => element.getClientRects().length > 0);
      return {
        header: header.getBoundingClientRect(),
        mark: mark.getBoundingClientRect(),
        controls: controls.map((element) => element.getBoundingClientRect()),
      };
    });
    const phoneReader = view === "reader" && width < 620;
    expect(geometry.header.height).toBe(phoneReader ? 44 : 56);
    if (width > 430 && !phoneReader) {
      expect(geometry.mark.x).toBe(20);
      expect(geometry.mark.y).toBe(18);
    } else {
      expect(geometry.mark.width).toBe(0);
      expect(geometry.mark.height).toBe(0);
    }
    expect(
      geometry.controls.every((control) =>
        width <= 430 || phoneReader
          ? control.height >= 44
          : control.height >= 30,
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
  await expect(page.locator(".chrome-overflow")).toBeHidden();
  await expect(page.locator(".reader-slot__text")).toBeVisible();

  await page.setViewportSize({ width: 900, height: 780 });
  await openFixture(page, "reader");
  await expect(
    page.locator(".chrome-group--secondary .chrome-btn__label").first(),
  ).toBeHidden();
  await expect(
    page.locator(".chrome-group--judge .chrome-btn__label").first(),
  ).toBeHidden();
  await expect(page.locator(".chrome-overflow")).toBeVisible();

  await page.setViewportSize({ width: 620, height: 780 });
  await openFixture(page, "reader");
  await expect(
    page.locator(".chrome-group--judge .chrome-btn__label").first(),
  ).toBeHidden();
  await expect(
    page.locator(".chrome-group--judge .chrome-btn__label").last(),
  ).toBeHidden();
  await expect(page.locator(".reader-slot__text")).toBeVisible();

  await page.setViewportSize({ width: 390, height: 780 });
  await openFixture(page, "grid");
  await expect(page.locator(".filter-button")).toBeVisible();
  await expect(page.locator(".header-segments")).toBeHidden();
});

test("phone reader exposes six native actions and clears the final line", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  await openFixture(page, "reader");
  await page.locator("#reader-last-line").waitFor();

  const actions = page.locator(".reader-bottom-actions button");
  await expect(actions).toHaveCount(6);
  expect(
    await actions.evaluateAll((buttons) =>
      buttons.map((button) => button.getAttribute("aria-label")),
    ),
  ).toEqual([
    "Boost (+)",
    "Bury (−)",
    "Keep in archive",
    "More actions",
    "Previous item",
    "Next unread item",
  ]);
  const heights = await actions.evaluateAll((buttons) =>
    buttons.map((button) => button.getBoundingClientRect().height),
  );
  expect(heights.every((height) => height >= 44)).toBe(true);
  await expect(page.locator(".reader-bottom-actions")).toHaveText("");

  const bar = page.locator(".reader-bottom-actions");
  const more = actions.nth(3);
  await page.locator(".reader-scroll").evaluate((element) => {
    element.scrollTop = Math.min(
      180,
      element.scrollHeight - element.clientHeight - 1,
    );
  });
  await expect(bar).toHaveClass(/collapsed/);
  await more.click();
  await expect(bar).not.toHaveClass(/collapsed/);
  await expect(page.locator(".reader-action-sheet")).toBeHidden();

  await more.click();
  const sheet = page.locator(".reader-action-sheet");
  await expect(sheet).toBeVisible();
  await expect(sheet.locator("button").first()).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(sheet).toBeHidden();
  await expect(more).toBeFocused();

  await page.locator(".reader-scroll").evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  const clearance = await page.evaluate(() => {
    const lastLine = document.querySelector("#reader-last-line");
    const actionBar = document.querySelector(".reader-bottom-actions");
    if (!lastLine || !actionBar)
      throw new Error("Phone reader fixture is incomplete");
    return (
      actionBar.getBoundingClientRect().top -
      lastLine.getBoundingClientRect().bottom
    );
  });
  expect(clearance).toBeGreaterThanOrEqual(0);
});

for (const width of [860, 900, 1024]) {
  test(`grid controls stay inside the header at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    await openFixture(page, "grid", "loaded");
    const bounds = await page.locator(".app-header").evaluate((header) =>
      [...header.querySelectorAll("button, a")]
        .filter(
          (element) =>
            element.getClientRects().length &&
            getComputedStyle(element).visibility !== "hidden",
        )
        .map((element) => ({
          label: element.getAttribute("aria-label") || element.textContent,
          left: element.getBoundingClientRect().left,
          right: element.getBoundingClientRect().right,
        })),
    );
    for (const control of bounds) {
      expect(control.left, control.label || "control").toBeGreaterThanOrEqual(
        20,
      );
      expect(control.right, control.label || "control").toBeLessThanOrEqual(
        width - 20,
      );
    }
    await expect(page.locator(".filter-button")).toBeVisible({
      visible: width < 1024,
    });
  });
}
