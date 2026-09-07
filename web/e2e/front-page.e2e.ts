import { expect, type Page, test } from "@playwright/test";

const published = "2026-09-04T18:00:00Z";
const item = (
  itemID: string,
  feedID: string,
  title: string,
  score: number,
  size: "S" | "M" | "L",
) => ({
  item_id: itemID,
  feed_id: feedID,
  feed_title: `Feed ${feedID}`,
  connector: "rss",
  url: `https://example.com/${itemID}`,
  title,
  summary: `Summary for ${title}`,
  summary_source: "feed",
  published_ts: published,
  fetched_ts: published,
  has_body: false,
  extract_quality: 0.8,
  score,
  size,
  read: false,
  signal: 0,
  hearted: false,
  ...(size === "L"
    ? { media_url: "/sema-mark.svg", media_w: 320, media_h: 180 }
    : {}),
});

async function stubFrontPage(
  page: Page,
  stories: unknown[],
  items: unknown[],
  readBatches: Array<{ ids: string[]; read: boolean }>,
  signals: Array<{ itemID: string; value: number }> = [],
  pagination: {
    nextCursor?: string;
    nextItems?: unknown[];
    page2Gate?: Promise<void>;
    onPage2Request?: () => void;
  } = {},
) {
  await page.clock.setFixedTime(new Date("2026-09-05T18:00:00Z"));
  await page.addInitScript(() => localStorage.setItem("sema.signed-in", "1"));
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/api/")) {
      await route.continue();
      return;
    }
    if (request.method() !== "GET") {
      if (url.pathname === "/api/items/read-batch") {
        const body = request.postDataJSON() as {
          ids: string[];
          read?: boolean;
        };
        readBatches.push({ ids: body.ids, read: body.read ?? true });
      }
      const signalMatch = url.pathname.match(/^\/api\/items\/([^/]+)\/signal$/);
      if (signalMatch) {
        const body = request.postDataJSON() as { value: number };
        signals.push({
          itemID: decodeURIComponent(signalMatch[1]),
          value: body.value,
        });
      }
      await route.fulfill({ status: 204 });
      return;
    }
    if (url.pathname === "/api/me") {
      await route.fulfill({
        json: {
          profile: {
            email: "reader@example.com",
            created_at: published,
            order_pref: "interest",
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
      return;
    }
    if (url.pathname === "/api/feeds") {
      await route.fulfill({ json: { feeds: [] } });
      return;
    }
    if (url.pathname === "/api/stories") {
      await route.fulfill({ json: { stories } });
      return;
    }
    if (url.pathname === "/api/items") {
      if (url.searchParams.has("cursor") && pagination.nextItems) {
        pagination.onPage2Request?.();
        await pagination.page2Gate;
        await route.fulfill({
          json: { items: pagination.nextItems, next_cursor: null },
        });
      } else {
        await route.fulfill({
          json: { items, next_cursor: pagination.nextCursor ?? null },
        });
      }
      return;
    }
    await route.fulfill({ json: {} });
  });
}

test.describe("mobile front-page tile grammar", () => {
  test.use({
    viewport: { width: 393, height: 852 },
    hasTouch: true,
    isMobile: true,
  });

  test("renders one two-up grammar and expands the full-width lead card", async ({
    page,
  }) => {
    const leadStory = {
      story_id: "mobile-lead-story",
      source_count: 4,
      order_key: 0.99,
      size: "L",
      items: [
        {
          ...item(
            "mobile-lead",
            "lead",
            "FBI Probes Service Selling 153M+ Drivers Licenses",
            0.99,
            "L",
          ),
          story_id: "mobile-lead-story",
        },
        ...[
          ["mobile-related-one", "9to5Mac", "First related headline"],
          ["mobile-related-two", "Techmeme", "Second related headline"],
          ["mobile-related-three", "The Verge", "Third related headline"],
        ].map(([itemID, feedID, title], index) => ({
          ...item(itemID, feedID, title, 0.9 - index * 0.01, "S"),
          story_id: "mobile-lead-story",
        })),
      ],
    };
    const imageTileStory = {
      story_id: "mobile-image-tile-story",
      source_count: 5,
      order_key: 0.98,
      size: "L",
      items: [
        {
          ...item(
            "mobile-image-tile",
            "overworld",
            "Until Dawn 2 Reveals Release Date in New Trailer",
            0.98,
            "L",
          ),
          story_id: "mobile-image-tile-story",
        },
        {
          ...item(
            "mobile-image-related",
            "ign",
            "Other game coverage",
            0.8,
            "S",
          ),
          story_id: "mobile-image-tile-story",
        },
        {
          ...item(
            "mobile-image-related-two",
            "gamespot",
            "Release date analysis",
            0.79,
            "S",
          ),
          story_id: "mobile-image-tile-story",
        },
        {
          ...item(
            "mobile-image-related-three",
            "polygon",
            "What the new trailer reveals",
            0.78,
            "S",
          ),
          story_id: "mobile-image-tile-story",
        },
      ],
    };
    const textTileStory = {
      story_id: "mobile-text-tile-story",
      source_count: 8,
      order_key: 0.97,
      size: "L",
      items: [
        {
          ...item(
            "mobile-text-tile",
            "maps",
            "Apple Maps changes name of Lake Ontario to Lake America",
            0.97,
            "L",
          ),
          story_id: "mobile-text-tile-story",
          media_url: undefined,
          media_w: undefined,
          media_h: undefined,
        },
        {
          ...item(
            "mobile-text-related",
            "mac",
            "Other maps coverage",
            0.79,
            "S",
          ),
          story_id: "mobile-text-tile-story",
        },
      ],
    };
    const singleSourceStory = {
      story_id: "mobile-single-source-story",
      source_count: 1,
      order_key: 0.96,
      size: "L",
      items: [
        {
          ...item(
            "mobile-single-source",
            "solo",
            "A single-source story",
            0.96,
            "L",
          ),
          story_id: "mobile-single-source-story",
        },
      ],
    };
    const extraTileStory = {
      story_id: "mobile-extra-tile-story",
      source_count: 2,
      order_key: 0.95,
      size: "L",
      items: [
        {
          ...item(
            "mobile-extra-tile",
            "extra",
            "Another tile story",
            0.95,
            "L",
          ),
          story_id: "mobile-extra-tile-story",
        },
        {
          ...item(
            "mobile-extra-related",
            "extra-related",
            "Another related headline",
            0.77,
            "S",
          ),
          story_id: "mobile-extra-tile-story",
        },
      ],
    };
    await page.addInitScript(() => localStorage.setItem("sema:theme", "dark"));
    await stubFrontPage(
      page,
      [
        leadStory,
        imageTileStory,
        textTileStory,
        singleSourceStory,
        extraTileStory,
      ],
      [item("mobile-tail-one", "tail-one", "Tail one", 0.5, "M")],
      [],
    );

    await page.goto("/");

    const leadCard = page.locator('[data-story-id="mobile-lead-story"]');
    await expect(leadCard).toHaveClass(/story-card/);
    await expect(
      leadCard.getByRole("img", { name: "4 sources" }),
    ).toBeVisible();
    await expect(leadCard.getByText("top 10%")).toBeHidden();
    const collapsedHeadlines = leadCard.locator(".story-headline");
    await expect(collapsedHeadlines).toHaveCount(1);
    expect(
      await collapsedHeadlines
        .first()
        .evaluate((element) => element.clientHeight),
    ).toBeGreaterThanOrEqual(44);

    for (const storyID of [
      "mobile-image-tile-story",
      "mobile-text-tile-story",
    ]) {
      const tile = page.locator(`[data-story-id="${storyID}"]`);
      await expect(tile).toHaveClass(/mobile-tile-cell/);
      await expect(tile).toHaveCSS("height", "196px");
      await expect(tile.locator(".story-stack-badge")).toBeVisible();
      await expect(tile.locator(".cell-age")).toBeVisible();
      await expect(tile.locator(".story-headlines")).toHaveCount(0);
    }
    const sourcesButton = page.getByRole("button", {
      name: "5 sources, show headlines",
    });
    const sourcesButtonBox = await sourcesButton.boundingBox();
    expect(sourcesButtonBox?.width).toBeGreaterThanOrEqual(44);
    expect(sourcesButtonBox?.height).toBeGreaterThanOrEqual(44);
    const singleSourceTile = page.locator(
      '[data-story-id="mobile-single-source-story"]',
    );
    await expect(
      singleSourceTile.getByRole("button", {
        name: "1 sources, show headlines",
      }),
    ).toHaveCount(0);
    expect(
      await singleSourceTile
        .locator(".story-stack-badge")
        .evaluate((element) => element.tagName),
    ).toBe("SPAN");

    const tileRow = page.locator(".grid-row").filter({
      has: page.locator('[data-story-id="mobile-image-tile-story"]'),
    });
    await tileRow.screenshot({ path: "/tmp/sema-mobile-grid-two-up.png" });
    await leadCard.screenshot({
      path: "/tmp/sema-mobile-lead-collapsed.png",
    });

    await leadCard.dispatchEvent("mouseover");
    await expect(leadCard).toHaveClass(/focused/);
    expect(
      await leadCard.evaluate(
        (element) => getComputedStyle(element).outlineStyle,
      ),
    ).toBe("none");

    await leadCard.getByRole("button", { name: "+2 more" }).click();
    await expect(leadCard.locator(".story-headline")).toHaveCount(3);
    await expect(
      leadCard.getByRole("button", { name: "Show less" }),
    ).toBeVisible();
    await expect(leadCard.locator(".story-headline-title")).toHaveText([
      "First related headline",
      "Second related headline",
      "Third related headline",
    ]);
    await leadCard.screenshot({
      path: "/tmp/sema-mobile-lead-expanded.png",
    });

    await leadCard.getByRole("button", { name: "Show less" }).click();
    await expect(leadCard.locator(".story-headline")).toHaveCount(1);
    await expect(
      leadCard.getByRole("button", { name: "+2 more" }),
    ).toBeVisible();

    await sourcesButton.click();
    await expect(page.locator(".reader-scroll")).toHaveCount(0);
    const sheet = page.getByRole("dialog", {
      name: "Actions for Until Dawn 2 Reveals Release Date in New Trailer",
    });
    await expect(sheet).toBeVisible();
    await expect(sheet).toContainText("5 sources · Feed overworld · 1d");
    const sheetHeadlines = sheet.locator(".sheet-headline");
    await expect(sheetHeadlines).toHaveCount(3);
    await expect(sheetHeadlines.locator(".story-headline-feed")).toHaveText([
      "Feed ign",
      "Feed gamespot",
      "Feed polygon",
    ]);
    await expect(sheetHeadlines.locator(".story-headline-title")).toHaveText([
      "Other game coverage",
      "Release date analysis",
      "What the new trailer reveals",
    ]);
    await expect(sheetHeadlines.locator("time")).toHaveText(["1d", "1d", "1d"]);
    await sheet.screenshot({
      path: "/tmp/sema-mobile-story-headlines-sheet.png",
    });

    await sheetHeadlines.nth(1).click();
    await expect(sheet).toHaveCount(0);
    await expect(page.locator(".reader-scroll")).toBeVisible();
    await expect(page.locator(".article h1")).toHaveText(
      "Release date analysis",
    );
  });
});

test("holds below-floor stories until page 2 without moving painted cells", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 5000 });
  const firstPage = Array.from({ length: 45 }, (_, index) =>
    item(
      `page-1-${index}`,
      `feed-${index}`,
      `Page 1 item ${index}`,
      1.3 - index * 0.01,
      "L",
    ),
  );
  const secondPage = Array.from({ length: 6 }, (_, index) =>
    item(
      `page-2-${index}`,
      `later-feed-${index}`,
      `Page 2 item ${index}`,
      0.6 - index * 0.01,
      "M",
    ),
  );
  const heldStory = {
    story_id: "held-story",
    source_count: 2,
    order_key: 0.7,
    size: "M",
    items: [
      {
        ...item("held-lead", "held-one", "Held story", 0.65, "M"),
        story_id: "held-story",
      },
      {
        ...item("held-headline", "held-two", "Held coverage", 0.64, "S"),
        story_id: "held-story",
      },
    ],
  };
  let releasePage2 = () => {};
  const page2Gate = new Promise<void>((resolve) => {
    releasePage2 = resolve;
  });
  let page2Requests = 0;
  await stubFrontPage(page, [heldStory], firstPage, [], [], {
    nextCursor: "page-2",
    nextItems: secondPage,
    page2Gate,
    onPage2Request: () => {
      page2Requests++;
    },
  });

  await page.goto("/");
  await expect(page.locator('[data-item-id="page-1-0"]')).toBeVisible();
  await expect.poll(() => page2Requests).toBe(1);
  await expect(page.locator('[data-story-id="held-story"]')).toHaveCount(0);

  const geometry = () =>
    page.locator("[data-item-id]").evaluateAll((cells) =>
      Object.fromEntries(
        cells.map((cell) => {
          const element = cell as HTMLElement;
          const row = element.closest<HTMLElement>(".grid-row");
          return [
            element.dataset.itemId ?? "",
            {
              top:
                (Number.parseFloat(row?.style.top ?? "") || 0) +
                element.offsetTop,
              left: element.offsetLeft,
              width: element.offsetWidth,
              height: element.offsetHeight,
            },
          ];
        }),
      ),
    );
  const before = await geometry();
  expect(Object.keys(before).length).toBeGreaterThan(0);

  releasePage2();
  await expect(page.locator('[data-story-id="held-story"]')).toBeVisible();
  await expect(page.locator('[data-item-id="page-2-0"]')).toBeVisible();
  const after = await geometry();

  expect(
    Object.fromEntries(
      Object.entries(before).filter(
        ([id, position]) =>
          JSON.stringify(after[id]) !== JSON.stringify(position),
      ),
    ),
  ).toEqual({});
});

test("M story cells use singleton anatomy and lead-scoped sheet actions", async ({
  page,
}) => {
  const lead = {
    ...item("m-lead", "one", "Medium story lead", 0.7, "M"),
    media_url: "/sema-mark.svg",
    media_w: 320,
    media_h: 180,
    story_id: "medium-story",
  };
  const headline = {
    ...item("m-headline", "two", "Other coverage", 0.6, "S"),
    story_id: "medium-story",
  };
  const story = {
    story_id: "medium-story",
    source_count: 2,
    order_key: 0.8,
    size: "M",
    items: [lead, headline],
  };
  const readBatches: Array<{ ids: string[]; read: boolean }> = [];
  const signals: Array<{ itemID: string; value: number }> = [];
  await stubFrontPage(
    page,
    [story],
    [item("singleton", "three", "Singleton", 0.5, "M")],
    readBatches,
    signals,
  );
  await page.goto("/");

  const storyCell = page.locator('[data-story-id="medium-story"]');
  await expect(storyCell.locator(":scope > img")).toHaveCount(1);
  await expect(storyCell.locator(".cell-copy h2")).toHaveText(
    "Medium story lead",
  );
  await expect(storyCell.locator(".cell-meta")).toContainText("Feed one");
  const storyBadge = storyCell.getByLabel("2 sources");
  await expect(storyBadge).toBeVisible();
  await expect(storyCell.locator(".story-badges")).toHaveCount(0);
  await expect(storyCell.locator(".cell-rank")).toHaveCount(0);
  await expect(storyCell.locator(".story-headlines")).toHaveCount(0);
  await page.screenshot({ path: "/tmp/sema-medium-story.png" });

  await storyCell.hover();
  await expect(storyBadge).toHaveCSS("opacity", "0");
  await storyBadge.click({ force: true });
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await storyCell.getByRole("button", { name: "More actions" }).click();
  const sheet = page.getByRole("dialog", {
    name: "Actions for Medium story lead",
  });
  await expect(sheet).toContainText("2 sources");
  const sheetHeadline = sheet.locator(".sheet-headline");
  await expect(sheetHeadline).toHaveCount(1);
  await expect(sheetHeadline.locator(".story-headline-feed")).toHaveText(
    "Feed two",
  );
  await expect(sheetHeadline.locator(".story-headline-title")).toHaveText(
    "Other coverage",
  );
  await expect(sheetHeadline.locator("time")).toHaveText("1d");
  await sheet.getByRole("button", { name: "Bury" }).click();
  await expect
    .poll(() => signals)
    .toContainEqual({
      itemID: "m-lead",
      value: -1,
    });

  await storyCell.hover();
  await storyCell.getByRole("button", { name: "More actions" }).click();
  await sheet.getByRole("button", { name: "Mark read" }).click();
  await expect
    .poll(() => readBatches.at(-1)?.ids)
    .toEqual(["m-lead", "m-headline"]);

  await storyCell.dispatchEvent("pointerdown", {
    pointerType: "touch",
    clientX: 20,
    clientY: 20,
  });
  await expect(sheet).toBeVisible();
});

test("editorial story actions match singleton hover behavior and light colors", async ({
  page,
}) => {
  const lead = {
    ...item("story-lead", "one", "Story lead", 0.8, "L"),
    story_id: "story-actions",
  };
  const story = {
    story_id: "story-actions",
    source_count: 2,
    order_key: 0.9,
    size: "L",
    items: [
      lead,
      {
        ...item("story-headline", "two", "Other coverage", 0.7, "S"),
        story_id: "story-actions",
      },
    ],
  };
  await page.addInitScript(() => localStorage.setItem("sema:theme", "light"));
  await stubFrontPage(
    page,
    [story],
    [
      {
        ...item("singleton", "three", "Singleton", 0.6, "M"),
        media_url: "/sema-mark.svg",
        media_w: 320,
        media_h: 180,
      },
    ],
    [],
  );
  await page.goto("/");

  const storyCell = page.locator('[data-story-id="story-actions"]');
  const storyActions = storyCell.locator(".story-actions");
  const singletonActions = page
    .locator('[data-item-id="singleton"]')
    .locator(".cell-actions");
  const singletonAge = page
    .locator('[data-item-id="singleton"]')
    .locator(".cell-age");
  const storyBadges = storyCell.locator(".story-badges");

  await expect(storyBadges).toHaveCSS("opacity", "1");
  await expect(storyActions).toHaveCSS("opacity", "0");
  await expect(storyActions).toHaveCSS("pointer-events", "none");

  const cellOffset = (element: typeof storyActions) =>
    element.evaluate((node) => {
      const cell = node.closest(".grid-cell");
      if (!cell) throw new Error("grid cell unavailable");
      const elementRect = node.getBoundingClientRect();
      const cellRect = cell.getBoundingClientRect();
      return {
        left: Math.round(elementRect.left - cellRect.left),
        top: Math.round(elementRect.top - cellRect.top),
      };
    });
  expect(await cellOffset(storyActions)).toEqual(
    await cellOffset(singletonActions),
  );

  await storyCell.hover();
  await expect(storyBadges).toHaveCSS("opacity", "0");
  await expect(storyActions).toHaveCSS("opacity", "1");
  await expect(storyActions).toHaveCSS("pointer-events", "auto");

  const actionColors = async (actions: typeof storyActions) =>
    actions.getByRole("button", { name: "More actions" }).evaluate((button) => {
      const style = getComputedStyle(button);
      return { background: style.backgroundColor, foreground: style.color };
    });
  expect(await actionColors(storyActions)).toEqual(
    await actionColors(singletonActions),
  );

  const labelColors = (label: typeof singletonAge) =>
    label.evaluate((element) => {
      const style = getComputedStyle(element);
      return { background: style.backgroundColor, foreground: style.color };
    });
  const singletonLabelColors = await labelColors(singletonAge);
  await expect
    .poll(() => labelColors(storyCell.locator(".story-badges span")))
    .toEqual(singletonLabelColors);
  await expect
    .poll(() => labelColors(storyCell.locator(".story-corner > span")))
    .toEqual(singletonLabelColors);
  expect(
    (await labelColors(storyCell.locator(".story-badges em"))).background,
  ).toBe(singletonLabelColors.background);

  const headline = storyCell.locator(".story-headline").first();
  await headline.hover();
  await expect(headline).toHaveClass(/focused/);
  const highlightColors = await headline.evaluate((element) => {
    const style = getComputedStyle(element);
    const resolveThemeColor = (property: string) => {
      const probe = document.createElement("span");
      probe.style.color = `var(${property})`;
      document.body.append(probe);
      const color = getComputedStyle(probe).color;
      probe.remove();
      return color;
    };
    return {
      background: style.backgroundColor,
      outline: style.outlineColor,
      selectedSurface: resolveThemeColor("--surface-selected"),
      accent: resolveThemeColor("--accent"),
    };
  });
  expect(highlightColors.background).toBe(highlightColors.selectedSurface);
  expect(highlightColors.outline).toBe(highlightColors.accent);

  await page.locator(".app-header").hover();
  await expect(storyBadges).toHaveCSS("opacity", "1");
  await expect(storyActions).toHaveCSS("opacity", "0");
  await storyActions.getByRole("button", { name: "More actions" }).focus();
  await expect(storyBadges).toHaveCSS("opacity", "0");
  await expect(storyActions).toHaveCSS("opacity", "1");
  await expect(storyActions).toHaveCSS("pointer-events", "auto");
});

test("stories earn their position in the interest grid", async ({ page }) => {
  const lead = {
    ...item("lead", "one", "Lead coverage", 0.7, "L"),
    story_id: "story-one",
  };
  const headline = {
    ...item("headline", "two", "Another source", 0.6, "S"),
    story_id: "story-one",
  };
  const story = {
    story_id: "story-one",
    source_count: 2,
    order_key: 0.8,
    size: "L",
    items: [lead, headline],
  };
  const large = item("large", "three", "Higher-interest singleton", 0.9, "L");
  const trailing = item(
    "trailing",
    "four",
    "Lower-interest singleton",
    0.5,
    "S",
  );
  const readBatches: Array<{ ids: string[]; read: boolean }> = [];
  await stubFrontPage(page, [story], [large, trailing], readBatches);

  await page.goto("/");
  const storyCell = page.locator('[data-story-id="story-one"]');
  const largeCell = page.locator('[data-item-id="large"]');
  const trailingCell = page.locator('[data-item-id="trailing"]');
  await expect(storyCell).toBeVisible();
  await expect(largeCell).toBeVisible();
  await expect(trailingCell).toBeVisible();
  const largeBox = await largeCell.boundingBox();
  const storyBox = await storyCell.boundingBox();
  const trailingBox = await trailingCell.boundingBox();
  expect(largeBox).not.toBeNull();
  expect(storyBox).not.toBeNull();
  expect(
    (largeBox?.y ?? 0) < (storyBox?.y ?? 0) ||
      ((largeBox?.y ?? 0) === (storyBox?.y ?? 0) &&
        (largeBox?.x ?? 0) < (storyBox?.x ?? 0)),
  ).toBe(true);
  expect(storyBox?.y).toBeLessThan(trailingBox?.y ?? 0);

  await expect(largeCell).toHaveClass(/focused/);
  await page.keyboard.press("j");
  await expect(storyCell).toHaveClass(/focused/);
  await page.keyboard.press("j");
  await expect(storyCell.locator('[data-focus-id="headline"]')).toHaveClass(
    /focused/,
  );

  await page.getByRole("radio", { name: "Latest", exact: true }).click();
  await expect(storyCell).toHaveCount(0);
  await page.getByRole("radio", { name: "Front page", exact: true }).click();
  await expect(storyCell).toBeVisible();

  for (const width of [1440, 1024, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(storyCell).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBeLessThanOrEqual(width);
    await page.screenshot({ path: `/tmp/sema-front-merged-${width}.png` });
  }
  await page.setViewportSize({ width: 768, height: 900 });
  await expect(storyCell.locator(".story-lead")).toBeVisible();

  await page.evaluate(() => {
    document.addEventListener(
      "click",
      (event) => {
        if (
          !(event.target instanceof Element) ||
          !event.target.closest(".story-lead")
        )
          return;
        requestAnimationFrame(() => {
          (
            window as typeof window & {
              __leadFirstFrame?: { readerVisible: boolean; storyRead: boolean };
            }
          ).__leadFirstFrame = {
            readerVisible: document.querySelector(".reader-scroll") !== null,
            storyRead:
              document
                .querySelector('[data-story-id="story-one"] .story-lead h2')
                ?.classList.contains("read") ?? false,
          };
        });
      },
      { capture: true, once: true },
    );
  });
  await storyCell.locator(".story-lead").click();
  await expect(page.locator(".reader-scroll")).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (
            window as typeof window & {
              __leadFirstFrame?: {
                readerVisible: boolean;
                storyRead: boolean;
              };
            }
          ).__leadFirstFrame,
      ),
    )
    .toEqual({ readerVisible: true, storyRead: false });
  await expect
    .poll(() => readBatches.map((batch) => batch.ids))
    .toContainEqual(["lead", "headline"]);
});

test("scroll-reading a story cell uses all of its unread members", async ({
  page,
}) => {
  const members = Array.from({ length: 8 }, (_, index) => ({
    ...item(
      `story-0-${index}`,
      `feed-${index}`,
      `Story coverage ${index}`,
      1 - index * 0.01,
      index === 0 ? "L" : "S",
    ),
    story_id: "story-0",
    read: index === 0,
  }));
  const story = {
    story_id: "story-0",
    source_count: 8,
    order_key: 1.2,
    size: "L",
    items: members,
  };
  const singletons = Array.from({ length: 30 }, (_, index) =>
    item(
      `singleton-${index}`,
      "single",
      `Singleton ${index}`,
      0.9 - index * 0.01,
      index % 5 === 0 ? "M" : "S",
    ),
  );
  const readBatches: Array<{ ids: string[]; read: boolean }> = [];
  await stubFrontPage(page, [story], singletons, readBatches);
  await page.goto("/");

  const storyCell = page.locator('[data-story-id="story-0"]');
  const scrollPastStoryRow = async () => {
    const scroller = page.locator(".grid-scroll");
    const delta = await storyCell.evaluate((element) => {
      const row = element.closest<HTMLElement>(".grid-row");
      const scroll = element.closest<HTMLElement>(".grid-scroll");
      if (!row || !scroll) throw new Error("story row unavailable");
      return Math.ceil(
        row.getBoundingClientRect().bottom -
          scroll.getBoundingClientRect().top +
          1,
      );
    });
    await scroller.hover();
    await page.mouse.wheel(0, delta);
    await expect
      .poll(() => scroller.evaluate((element) => element.scrollTop))
      .toBeGreaterThanOrEqual(delta);
  };

  await page.getByRole("radio", { name: "All", exact: true }).click();
  await scrollPastStoryRow();
  await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
  await page.waitForTimeout(100);
  expect(readBatches).toEqual([]);

  await page.getByRole("radio", { name: "Unread", exact: true }).click();
  await expect
    .poll(() =>
      page.locator(".grid-scroll").evaluate((element) => element.scrollTop),
    )
    .toBe(0);
  await scrollPastStoryRow();
  const unreadMemberIDs = members.slice(1).map((member) => member.item_id);
  await expect(storyCell.locator(".story-headline").first()).toHaveClass(
    /read/,
  );
  await page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
  await expect
    .poll(() =>
      unreadMemberIDs.every((id) => readBatches.at(-1)?.ids.includes(id)),
    )
    .toBe(true);
  expect(readBatches.at(-1)?.ids).not.toContain(members[0].item_id);
  await page.keyboard.press("u");
  await expect(storyCell.locator(".story-lead h2")).toHaveClass(/read/);
});

test("story read visuals follow the grid's All and Unread contexts", async ({
  page,
}) => {
  const members = [
    {
      ...item("read-lead", "read-one", "Read lead", 1, "L"),
      story_id: "read-story",
      read: true,
    },
    {
      ...item("read-headline-1", "read-two", "Read headline one", 0.9, "S"),
      story_id: "read-story",
      read: true,
    },
    {
      ...item("read-headline-2", "read-three", "Read headline two", 0.8, "S"),
      story_id: "read-story",
      read: true,
    },
  ];
  const story = {
    story_id: "read-story",
    source_count: 3,
    order_key: 1.2,
    size: "L",
    items: members,
  };
  const companions = Array.from({ length: 3 }, (_, index) =>
    item(`read-companion-${index}`, "single", `Companion ${index}`, 0.7, "S"),
  );
  await stubFrontPage(page, [story], companions, []);
  await page.goto("/");

  await page.getByRole("radio", { name: "All", exact: true }).click();
  const storyCell = page.locator('[data-story-id="read-story"]');
  const leadTitle = storyCell.locator(".story-lead h2");
  const headlines = storyCell.locator(".story-headline");
  await expect(headlines).toHaveCount(2);
  await expect(storyCell).not.toHaveClass(/\bread\b/);
  await expect(leadTitle).not.toHaveClass(/\bread\b/);
  await expect(headlines.first()).not.toHaveClass(/\bread\b/);
  await expect(storyCell.locator(".unread-dot")).toHaveCount(0);

  await page.getByRole("radio", { name: "Unread", exact: true }).click();
  await expect(headlines.first()).toHaveClass(/\bread\b/);
  await expect(headlines.first().locator(".unread-dot")).toHaveCount(0);
});

test("desktop story titles and summaries fit their cards after resizing and expansion", async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem("sema:theme", "dark"));
  const titles = [
    "The car industry A/B tested selling a car with and without CarPlay",
    "Double Fine’s first game since leaving Xbox is a comedy bus-driving sim called Thank You Bus Driver",
    "Lululemon founder Chip Wilson and wife Shannon Wilson are divorcing after building a business together over several decades",
    "A story without an image still needs enough room for its complete headline, source, and any summary that can fit",
    "A deliberately long headline about the car industry testing the same vehicle with and without CarPlay, what drivers chose, and why those results could change the design of the next generation of vehicles",
    "A single-source story with an unusually long title should also remain readable when the grid is resized to a narrower desktop window",
    "A short story title",
  ];
  const stories = titles.map((title, index) => {
    const storyID = `sizing-story-${index}`;
    const relatedCount = [1, 2, 1, 2, 5, 0, 0][index];
    return {
      story_id: storyID,
      source_count: relatedCount + 1,
      order_key: 1 - index * 0.05,
      size: "L",
      items: [
        {
          ...item(`sizing-lead-${index}`, "lead", title, 1 - index * 0.05, "L"),
          story_id: storyID,
          summary:
            "Article URL: https://example.com/a-long-article-address-that-should-wrap-without-leaking-out-of-the-card Comments URL: https://example.com/discussion More details about the story appear here.",
          ...(index === 3 ? { media_url: undefined } : {}),
        },
        ...Array.from({ length: relatedCount }, (_, related) => ({
          ...item(
            `sizing-related-${index}-${related}`,
            "related",
            "Another source explains what drivers chose and why CarPlay matters",
            0.5,
            "S",
          ),
          feed_title: "Daring Fireball",
          story_id: storyID,
        })),
      ],
    };
  });
  await stubFrontPage(page, stories, [], []);
  await page.setViewportSize({ width: 1500, height: 2200 });
  await page.goto("/");
  const cards = page.locator(".story-card");
  await expect(cards).toHaveCount(stories.length);
  await page.evaluate(() => document.fonts.ready);

  const clipping = () =>
    cards.evaluateAll((elements) => {
      const errors: string[] = [];
      for (const card of elements) {
        const shell = card.querySelector(".story-lead-shell");
        if (!shell) throw new Error("Missing story lead");
        const bounds = shell.getBoundingClientRect();
        for (const selector of ["h2", "p", ".story-meta"]) {
          const element = shell.querySelector<HTMLElement>(selector);
          if (!element || getComputedStyle(element).display === "none")
            continue;
          const rect = element.getBoundingClientRect();
          if (rect.top < bounds.top || rect.bottom > bounds.bottom + 1)
            errors.push(
              `${card.getAttribute("data-story-id")}: clipped ${selector}`,
            );
          if (element.scrollWidth > element.clientWidth + 1)
            errors.push(
              `${card.getAttribute("data-story-id")}: wide ${selector}`,
            );
          if (selector === "p") {
            const lineHeight = Number.parseFloat(
              getComputedStyle(element).lineHeight,
            );
            if (
              Math.abs(
                rect.height / lineHeight - Math.round(rect.height / lineHeight),
              ) > 0.02
            )
              errors.push("Summary ends with a partial line");
          }
        }
        const headlines = card.querySelector(".story-headlines");
        if (
          headlines &&
          headlines.getBoundingClientRect().bottom >
            card.getBoundingClientRect().bottom + 1
        )
          errors.push("Related headlines extend beyond the card");
      }
      const boxes = elements.map((element) => element.getBoundingClientRect());
      for (let i = 0; i < boxes.length; i++) {
        for (let j = i + 1; j < boxes.length; j++) {
          const a = boxes[i];
          const b = boxes[j];
          if (
            a.left < b.right &&
            b.left < a.right &&
            a.top < b.bottom &&
            b.top < a.bottom
          )
            errors.push("Story cards overlap");
        }
      }
      return errors;
    });

  for (const width of [1500, 900, 760, 1600]) {
    await page.setViewportSize({ width, height: 2200 });
    await expect.poll(clipping).toEqual([]);
    await expect(cards.locator("h2")).toHaveText(titles);
  }
  await expect(
    page.locator('[data-story-id="sizing-story-6"] .story-lead p'),
  ).toBeVisible();
  const relatedCopy = cards.first().locator(".story-headline-copy");
  await expect(relatedCopy).toBeVisible();
  expect(
    await relatedCopy.evaluate((element) => {
      const height = element.getBoundingClientRect().height;
      return (
        height > Number.parseFloat(getComputedStyle(element).lineHeight) * 1.5
      );
    }),
  ).toBe(true);
  await page.screenshot({
    path: "/tmp/sema-story-titles-fixed.png",
    clip: { x: 0, y: 0, width: 1600, height: 900 },
  });

  const expandable = page.locator('[data-story-id="sizing-story-4"]');
  await expandable.getByRole("button", { name: /more$/ }).click();
  await expect(expandable.locator(".story-headline")).toHaveCount(5);
  await expect.poll(clipping).toEqual([]);
});
