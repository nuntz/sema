import { expect, type Page, test } from "@playwright/test";
import { nextPageTop } from "./grid-paging";

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

    await page.screenshot({ path: "/tmp/sema-mobile-initial.png" });
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
      await expect(tile).toHaveCSS("height", "260px");
      await expect(tile.locator(".story-stack-badge")).toBeVisible();
      await expect(tile.locator(".refined-age")).toBeVisible();
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
    await expect(leadCard.locator(".story-related-title")).toHaveText([
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
    await expect(sheetHeadlines.locator(".story-related-source")).toHaveText([
      "Feed ign · 1d",
      "Feed gamespot · 1d",
      "Feed polygon · 1d",
    ]);
    await expect(sheetHeadlines.locator(".story-related-title")).toHaveText([
      "Other game coverage",
      "Release date analysis",
      "What the new trailer reveals",
    ]);

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
  await storyCell.getByRole("button", { name: "More actions" }).focus();
  await page.keyboard.press("Enter");
  const sheet = page.getByRole("dialog", {
    name: "Actions for Medium story lead",
  });
  await expect(sheet).toContainText("2 sources");
  const sheetHeadline = sheet.locator(".sheet-headline");
  await expect(sheetHeadline).toHaveCount(1);
  await expect(sheetHeadline.locator(".story-related-source")).toHaveText(
    "Feed two · 1d",
  );
  await expect(sheetHeadline.locator(".story-related-title")).toHaveText(
    "Other coverage",
  );

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
  const storyBadges = storyCell.locator(".story-badges");

  await expect(storyBadges).toBeHidden();
  await expect(storyActions.locator("button.more")).toHaveCSS("opacity", "0");
  await expect(storyActions.locator("button.more")).toHaveCSS(
    "pointer-events",
    "none",
  );

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
  await expect(storyBadges).toBeHidden();
  await expect(storyActions.locator("button.more")).toHaveCSS("opacity", "1");
  await expect(storyActions.locator("button.more")).toHaveCSS(
    "pointer-events",
    "auto",
  );

  const actionColors = async (actions: typeof storyActions) =>
    actions.getByRole("button", { name: "More actions" }).evaluate((button) => {
      const style = getComputedStyle(button);
      return { background: style.backgroundColor, foreground: style.color };
    });
  expect(await actionColors(storyActions)).toEqual(
    await actionColors(singletonActions),
  );

  const headline = storyCell.locator(".story-headline").first();
  await headline.hover();
  await expect(headline).toHaveClass(/focused/);
  await expect(headline).toHaveCSS("background-color", "rgb(239, 236, 229)");
  await expect(headline).toHaveCSS("outline-style", "none");
  await headline.focus();
  await page.keyboard.press("ArrowUp");
  await page.keyboard.press("ArrowDown");
  await expect(headline).toHaveCSS("outline-style", "solid");
  await expect(headline).toBeFocused();
  // Let both queued arrow-key focus moves settle before leaving the grid.
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );

  await page.locator(".app-header a").first().focus();
  await page.locator(".app-header").hover();
  await expect(storyBadges).toBeHidden();
  await expect(storyActions.locator("button.more")).toHaveCSS("opacity", "0");
  await storyActions.getByRole("button", { name: "More actions" }).focus();
  await expect(storyBadges).toBeHidden();
  await expect(storyActions.locator("button.more")).toHaveCSS("opacity", "1");
  await expect(storyActions.locator("button.more")).toHaveCSS(
    "pointer-events",
    "auto",
  );
});

for (const selector of [".story-lead", ".story-headline"]) {
  test(`Space pages the grid from a focused ${selector}`, async ({ page }) => {
    const lead = {
      ...item("lead", "one", "Lead coverage", 0.9, "L"),
      story_id: "story-one",
    };
    const headline = {
      ...item("headline", "two", "Another source", 0.8, "S"),
      story_id: "story-one",
    };
    await stubFrontPage(
      page,
      [
        {
          story_id: "story-one",
          source_count: 2,
          order_key: 0.9,
          size: "L",
          items: [lead, headline],
        },
      ],
      Array.from({ length: 20 }, (_, index) =>
        item(`trailing-${index}`, "three", `Trailing item ${index}`, 0.5, "S"),
      ),
      [],
    );
    await page.goto("/");
    const control = page.locator(selector).first();
    await control.focus();
    await expect(control).toBeFocused();
    const grid = page.locator(".grid-scroll");
    const top = await grid.evaluate((element) => element.scrollTop);
    const target = await nextPageTop(grid);
    await page.keyboard.press("Space");
    await expect
      .poll(() => grid.evaluate((element) => element.scrollTop))
      .toBe(target);
    await expect(page.locator(".reader")).toHaveCount(0);
    await page.keyboard.press("Shift+Space");
    await expect
      .poll(() => grid.evaluate((element) => element.scrollTop))
      .toBe(top);
    await expect(page.locator(".reader")).toHaveCount(0);
  });
}

for (const width of [1024, 768]) {
  test(`arrows follow visual rows with an offscreen story at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const lead = {
      ...item("lead", "one", "Offscreen story", 0.1, "L"),
      story_id: "story-one",
    };
    await stubFrontPage(
      page,
      [
        {
          story_id: "story-one",
          source_count: 1,
          order_key: 0.1,
          size: "L",
          items: [lead],
        },
      ],
      Array.from({ length: 40 }, (_, index) =>
        item(
          `cell-${index}`,
          "two",
          `Grid cell ${index}`,
          0.9 - index / 100,
          "S",
        ),
      ),
      [],
    );
    await page.goto("/");
    const row = page.locator(".grid-row").nth(1);
    await expect(row.locator(".grid-cell").nth(2)).toBeVisible();
    const cell = row
      .locator(".grid-cell")
      .nth(Math.floor((await row.locator(".grid-cell").count()) / 2));
    for (const key of [
      "ArrowUp",
      "k",
      "ArrowDown",
      "j",
      "ArrowLeft",
      "ArrowRight",
    ]) {
      await page.keyboard.press("Home");
      await expect(page.locator(".grid-cell.focused")).toHaveAttribute(
        "data-item-id",
        "cell-0",
      );
      for (
        let column = 0;
        column < Math.floor((await row.locator(".grid-cell").count()) / 2);
        column++
      ) {
        await page.keyboard.press("ArrowRight");
        await expect(
          page.locator(".grid-cell.focused .cell-main"),
        ).toBeFocused();
      }
      await page.keyboard.press("ArrowDown");
      await expect(cell).toHaveClass(/focused/);
      const before = await cell.boundingBox();
      if (!before) throw new Error("missing origin cell");
      await page.keyboard.press(key);
      const destination = page.locator(".grid-cell.focused");
      await expect(destination.locator(".cell-main")).toBeFocused();
      const after = await destination.boundingBox();
      if (!after) throw new Error("missing destination cell");
      if (key === "ArrowUp" || key === "k")
        expect(after.y).toBeLessThan(before.y);
      else if (key === "ArrowDown" || key === "j")
        expect(after.y).toBeGreaterThan(before.y);
      else {
        expect(after.y).toBe(before.y);
        if (key === "ArrowLeft") expect(after.x).toBeLessThan(before.x);
        else expect(after.x).toBeGreaterThan(before.x);
      }
    }
  });
}

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
  await storyCell.locator(".story-lead").focus();
  await expect(storyCell).toHaveClass(/focused/);
  await page.keyboard.press("ArrowDown");
  await expect(storyCell.locator('[data-focus-id="headline"]')).toHaveClass(
    /focused/,
  );
  await expect(storyCell.locator('[data-focus-id="headline"]')).toBeFocused();
  await page.keyboard.press("ArrowUp");
  await expect(storyCell.locator(".story-lead")).toBeFocused();

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

test("dark story read visuals follow the grid's All and Unread contexts", async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem("sema:theme", "dark"));
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
  await storyCell.getByRole("button", { name: /\+\d+ more/ }).click();
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
            "Another source explains what drivers chose, why CarPlay matters, and what the car industry learned from testing vehicles with and without it",
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
  const relatedCopy = cards.first().locator(".story-related-title");
  await expect(relatedCopy).toBeVisible();
  await expect
    .poll(() =>
      relatedCopy.evaluate((element) => {
        const height = element.getBoundingClientRect().height;
        return (
          height > Number.parseFloat(getComputedStyle(element).lineHeight) * 1.5
        );
      }),
    )
    .toBe(true);
  await page.screenshot({
    path: "/tmp/sema-story-titles-fixed.png",
    clip: { x: 0, y: 0, width: 1600, height: 900 },
  });

  const expandable = page.locator('[data-story-id="sizing-story-4"]');
  await expandable.getByRole("button", { name: /more$/ }).click();
  await expect(expandable.locator(".story-headline")).toHaveCount(5);
  await expect.poll(clipping).toEqual([]);
});

for (const alreadyRead of [false, true]) {
  test(`finish and clear removes story cells and undo restores them (${alreadyRead ? "read" : "unread"})`, async ({
    page,
  }) => {
    const story = {
      story_id: "clear-story",
      source_count: 2,
      order_key: 0.8,
      size: "M",
      items: [
        {
          ...item("clear-lead", "one", "Story to clear", 0.8, "M"),
          read: alreadyRead,
        },
        {
          ...item("clear-member", "two", "Related coverage", 0.7, "S"),
          read: alreadyRead,
        },
      ],
    };
    await stubFrontPage(
      page,
      [story],
      [item("clear-singleton", "three", "Ordinary item", 0.5, "M")],
      [],
    );
    await page.goto("/");
    const storyCell = page.locator('[data-story-id="clear-story"]');
    await expect(storyCell).toBeVisible();
    await expect(
      page.locator('[data-item-id="clear-singleton"]'),
    ).toBeVisible();
    await page.getByRole("button", { name: /Mark \d+ read & clear/ }).click();
    await expect(page.locator(".grid-cell")).toHaveCount(0);
    await expect(
      page.getByRole("heading", { name: "You're all caught up" }),
    ).toBeVisible();
    await expect(page.locator(".grid-scroll")).toBeFocused();
    await page.keyboard.press("u");
    await expect(storyCell).toBeVisible();
    await expect(
      page.locator('[data-item-id="clear-singleton"]'),
    ).toBeVisible();
    await expect(
      page.getByRole("button", {
        name: `Mark ${alreadyRead ? 1 : 3} read & clear`,
      }),
    ).toBeVisible();
  });
}

test("Direction A keeps light stories readable and related coverage actionable", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1513, height: 1071 });
  await page.emulateMedia({ colorScheme: "light" });
  const titles = [
    "Smartphone makers don't bother to comply with EU repairability requirements",
    "SpaceX rival launches rocket in historic first, says industry is ‘desperate’ for more",
    "EU faces 300,000 factory job cuts as China ‘colonises’ supply chains, industry warns",
    "bzip3",
    "Railtown: Rethinking Vancouver's industrial-creative enclave, without creating ‘another Yaletown’",
    "Conquering Entropy: Cultivating Trust",
    "Sony Is Reportedly Bringing Killzone Back Over A Decade Later",
    "Onimusha Sells 1 million in a Day as Capcom Vows to ‘Re-activate’ More Old Series",
    "Final Fantasy Resonance Mod Brings Back Classic FF Character Ariana Grande",
  ];
  const stories = titles.map((title, index) => ({
    story_id: `light-${index}`,
    source_count: 2,
    order_key: 1 - index * 0.05,
    size: "L",
    items: [
      {
        ...item(`light-lead-${index}`, "lead", title, 1 - index * 0.05, "L"),
        feed_title: index === 0 ? "Hacker News: Front Page" : "Ars Technica",
        media_url: `/light-fixture-${index}.svg`,
        summary: "",
        read: index === 1,
      },
      {
        ...item(
          `light-related-${index}`,
          "related",
          index === 4
            ? title.replace("Vancouver's", "Vancouver&#39;s")
            : index === 1
              ? "German company becomes first in Europe to launch fully commercial orbital rocket"
              : title,
          0.4,
          "S",
        ),
        feed_title:
          index === 0 ? "www.theregister.com - Articles" : "Vancouver Sun",
        read: index === 1,
      },
    ],
  }));
  await page.route("**/light-fixture-*.svg", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="600" height="200"><rect width="600" height="200" fill="#cad6d9"/><path d="M0 160L160 40L340 180L510 50L600 100V200H0Z" fill="#94abad"/></svg>',
    }),
  );
  await stubFrontPage(page, stories, [], []);
  await page.goto("/");
  await page.getByRole("radio", { name: "All", exact: true }).click();
  const grid = page.locator(".grid-scroll");
  const first = page.locator('[data-story-id="light-0"]');
  const related = first.locator(".story-headline");
  await expect(grid).toHaveClass(/refined-grid/);
  await expect(first.locator(".story-meta .unread-dot")).toBeVisible();
  await expect(
    page.locator('[data-story-id="light-1"] .story-meta .unread-dot'),
  ).toHaveCount(0);
  await expect(first).toHaveCSS("background-color", "rgb(255, 255, 255)");
  await expect(first.locator(".story-badges")).toBeHidden();
  await expect(first.locator(".story-corner")).toBeHidden();
  await expect(first.locator(".story-meta")).toContainText("Hacker News");
  await expect(related).toHaveText(/Also covered by The Register/);
  await expect(related).toHaveAccessibleName(`Open ${titles[0]}`);
  await expect(
    page.locator('[data-story-id="light-1"] .story-related-title'),
  ).toContainText("German company");
  await expect(page.locator('[data-story-id="light-1"]')).toHaveCSS(
    "opacity",
    "1",
  );
  const readStory = page.locator('[data-story-id="light-1"]');
  await expect(readStory).toHaveCSS("background-color", "rgb(255, 255, 255)");
  await expect(readStory.locator(".story-lead h2")).toHaveCSS(
    "color",
    "rgb(20, 22, 26)",
  );
  await expect(readStory.locator(".story-media")).toHaveCSS("filter", "none");
  await expect(
    page.locator('[data-story-id="light-1"] .refined-read-label'),
  ).toContainText("read");
  await expect(
    page.locator('[data-story-id="light-4"] .related-also'),
  ).toBeVisible();
  await first.hover();
  await expect(first).toHaveCSS("outline-style", "none");
  await expect(first.locator(".ranking-hint")).toHaveCSS("opacity", "1");
  await page.locator(".app-header").hover();
  await expect(first.locator(".ranking-hint")).toHaveCSS("opacity", "0");
  await page.screenshot({ path: "/tmp/sema-direction-a-desktop.png" });
  await first.locator(".story-lead").focus();
  await page.keyboard.press("ArrowDown");
  await expect(related).toBeFocused();
  await expect(related).toHaveCSS("outline-style", "solid");
  await page.keyboard.press("Enter");
  await expect(page.locator(".reader")).toBeVisible();
  await page.keyboard.press("Escape");
  for (const width of [1024, 768, 720, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(first).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth),
    ).toBeLessThanOrEqual(width);
    if (width >= 768) {
      await expect(first.locator(".story-media-action")).toHaveCSS(
        "height",
        "126px",
      );
      await expect(first.locator(".story-headline")).toBeVisible();
    } else {
      await expect(grid).toHaveClass(/mobile-refined-grid/);
    }
  }
  await page.setViewportSize({ width: 1513, height: 1071 });
  await page.getByRole("radio", { name: "Unread", exact: true }).click();
  await expect(grid.locator(".unread-dot")).toHaveCount(0);
  await expect(readStory).toHaveCSS("background-color", "rgb(247, 245, 241)");
  await expect(readStory.locator(".story-lead h2")).toHaveCSS(
    "color",
    "rgb(92, 97, 105)",
  );
  await expect(readStory.locator(".story-media")).toHaveCSS(
    "filter",
    "opacity(0.55)",
  );
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(grid).toHaveClass(/refined-grid/);
  await expect(first.locator(".story-badges")).toBeHidden();
  await expect(first).toHaveCSS("background-color", "rgb(23, 24, 26)");
  await expect(first.locator(".story-headlines")).toHaveCSS(
    "background-color",
    "rgb(16, 17, 19)",
  );
  await expect(readStory).toHaveCSS("background-color", "rgb(16, 17, 19)");
  await expect(readStory.locator(".story-headlines")).toHaveCSS(
    "background-color",
    "rgb(11, 12, 14)",
  );
  await page.emulateMedia({ colorScheme: "light" });
  await expect(first).toHaveCSS("background-color", "rgb(255, 255, 255)");
  await expect(first.locator(".story-headlines")).toHaveCSS(
    "background-color",
    "rgb(250, 249, 245)",
  );
});

test("Direction A fits singleton headlines and metadata in compact cards", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1513, height: 1071 });
  await page.addInitScript(() => localStorage.setItem("sema:theme", "light"));
  const items = Array.from({ length: 12 }, (_, index) => ({
    ...item(
      `compact-light-${index}`,
      "feed",
      "A long headline about the next generation of software and the people building it",
      0.8 - index * 0.01,
      index % 2 ? "S" : "M",
    ),
    feed_title: "Hacker News: Front Page",
    summary: "",
    read: index === 0,
    media_url: "/sema-mark.svg",
    media_w: 320,
    media_h: 180,
  }));
  await stubFrontPage(page, [], items, []);
  await page.goto("/");
  await page.getByRole("radio", { name: "All", exact: true }).click();
  for (const width of [1513, 1024, 768]) {
    await page.setViewportSize({ width, height: 1071 });
    const first = page.locator('[data-item-id="compact-light-0"]');
    await expect(first).toBeVisible();
    await expect(first.locator(".cell-copy h2")).toHaveCSS(
      "color",
      "rgb(20, 22, 26)",
    );
    await expect(first).toHaveCSS("background-color", "rgb(255, 255, 255)");
    await expect(first.locator(":scope > img")).toHaveCSS("filter", "none");
    await expect(first.locator(".cell-corner")).toBeHidden();
    await expect(first.locator(".refined-age")).toHaveCSS(
      "color",
      "rgb(92, 97, 105)",
    );
    await expect
      .poll(() =>
        page.locator(".grid-cell").evaluateAll((cells) =>
          cells.every((cell) => {
            const title = cell.querySelector("h2")?.getBoundingClientRect();
            const meta = cell
              .querySelector(".cell-meta")
              ?.getBoundingClientRect();
            const bounds = cell.getBoundingClientRect();
            return (
              title &&
              meta &&
              title.bottom <= meta.top + 1 &&
              meta.bottom < bounds.bottom
            );
          }),
        ),
      )
      .toBe(true);
  }
  await page.screenshot({ path: "/tmp/sema-direction-a-compact.png" });
  await page.locator(".filter-button").click();
  await page.getByRole("radio", { name: "Unread", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect(page.locator(".grid-scroll .unread-dot")).toHaveCount(0);
  const sessionRead = page.locator('[data-item-id="compact-light-1"]');
  await sessionRead.hover();
  await page.keyboard.press("m");
  await page.locator(".app-header").hover();
  await expect(sessionRead).toHaveCSS("background-color", "rgb(247, 245, 241)");
  await expect(sessionRead.locator(".cell-copy h2")).toHaveCSS(
    "color",
    "rgb(92, 97, 105)",
  );
  await expect(sessionRead.locator(":scope > img")).toHaveCSS(
    "filter",
    "opacity(0.55)",
  );
});

test("light singleton photos use spare height while keeping copy together", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1513, height: 1071 });
  await page.addInitScript(() => localStorage.setItem("sema:theme", "light"));
  const titles = [
    "Letter from Paris | Paris, people and power",
    "WhatsApp will soon let users chat with up to five third-party AI agents",
    "UBC and Langara College formalize partnership to strengthen student pathways and success",
    "The complex corporate web behind a $3.2 billion AI data center",
    "Reports describe two ways for Apple to make more money: only one is good",
    "Oil prices rise to 6-week high after Iran and U.S. trade blows",
  ];
  const items = titles.map((title, index) => ({
    ...item(`photo-space-${index}`, "feed", title, 0.9 - index * 0.01, "L"),
    summary: "",
    media_w: 600,
    media_h: 400,
    ...(index === 2
      ? {
          connector: "reddit",
          post_type: "link",
          external_url: "https://news.ubc.ca/example",
          feed_title: "r/vancouver",
        }
      : {}),
  }));
  await stubFrontPage(page, [], items, []);
  await page.goto("/");
  await expect(page.locator(".grid-cell")).toHaveCount(items.length);
  await expect(page.locator(".grid-scroll .unread-dot")).toHaveCount(0);
  await page.getByRole("radio", { name: "All", exact: true }).click();
  for (const width of [1513, 1024, 768]) {
    await page.setViewportSize({ width, height: 1071 });
    await expect(
      page.locator('[data-item-id="photo-space-2"] .cell-meta .unread-dot'),
    ).toHaveCSS("width", "6px");
    await expect
      .poll(() =>
        page.locator(".grid-cell").evaluateAll((cells) =>
          cells.every((cell) => {
            const img = cell
              .querySelector(":scope > img")
              ?.getBoundingClientRect();
            const copy = cell
              .querySelector(".cell-copy")
              ?.getBoundingClientRect();
            const title = cell.querySelector("h2")?.getBoundingClientRect();
            const meta = cell
              .querySelector(".cell-meta")
              ?.getBoundingClientRect();
            const lastText =
              cell.querySelector(".reddit-domain")?.getBoundingClientRect() ??
              title;
            const bounds = cell.getBoundingClientRect();
            return (
              img &&
              copy &&
              title &&
              meta &&
              lastText &&
              img.height >= Math.min(126, bounds.height * 0.4) - 1 &&
              Math.abs(img.bottom - copy.top) < 1 &&
              title.bottom <= meta.top &&
              meta.top - lastText.bottom < 20 &&
              meta.bottom < bounds.bottom
            );
          }),
        ),
      )
      .toBe(true);
  }
  await page.setViewportSize({ width: 1513, height: 1071 });
  await page.screenshot({ path: "/tmp/sema-direction-a-singleton-photos.png" });
  await page.locator('[data-item-id="photo-space-0"] .cell-main').click();
  await expect(page.locator(".reader")).toBeVisible();
});

test("delayed singleton photos fill their cards after resizing and scrolling", async ({
  page,
}, testInfo) => {
  await page.setViewportSize({ width: 1470, height: 833 });
  await page.addInitScript(() => localStorage.setItem("sema:theme", "light"));
  await page.addInitScript(() => {
    HTMLImageElement.prototype.decode = function () {
      this.dataset.decodeAttempted = "true";
      return Promise.resolve();
    };
  });
  let releaseImages = () => {};
  const imagesReady = new Promise<void>((resolve) => {
    releaseImages = resolve;
  });
  await page.route("**/delayed-grid-*.svg", async (route) => {
    await imagesReady;
    await route.fulfill({
      contentType: "image/svg+xml",
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="480"><rect width="640" height="240" fill="teal"/><rect y="240" width="640" height="240" fill="coral"/></svg>',
    });
  });
  const items = Array.from({ length: 48 }, (_, index) => ({
    ...item(`delayed-${index}`, "feed", `Delayed photo ${index}`, 0.9, "L"),
    media_url: `/delayed-grid-${index}.svg`,
    media_w: 640,
    media_h: 480,
  }));
  await stubFrontPage(page, [], items, []);
  try {
    await page.goto("/", { waitUntil: "domcontentloaded" });
    const photo = page.locator('[data-item-id="delayed-0"] > img');
    await expect(photo).toBeAttached();
    expect(await photo.evaluate((img: HTMLImageElement) => img.complete)).toBe(
      false,
    );
    await page.setViewportSize({ width: 1024, height: 833 });
    releaseImages();
    const checkPhoto = async () => {
      await expect
        .poll(() =>
          photo.evaluate((img: HTMLImageElement) => {
            const bounds = img.getBoundingClientRect();
            const copy = img.parentElement
              ?.querySelector(".cell-copy")
              ?.getBoundingClientRect();
            return (
              img.complete &&
              img.dataset.decodeAttempted === "true" &&
              img.naturalWidth === 640 &&
              bounds.height >= 125 &&
              !!copy &&
              Math.abs(bounds.bottom - copy.top) < 1
            );
          }),
        )
        .toBe(true);
      await expect(photo).toHaveCSS("opacity", "1");
    };
    await checkPhoto();
    const grid = page.locator(".grid-scroll");
    await grid.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect(photo).toHaveCount(0);
    await grid.evaluate((element) => {
      element.scrollTop = 0;
    });
    await checkPhoto();
    await page.setViewportSize({ width: 1470, height: 833 });
    await checkPhoto();
    await page.screenshot({
      path: testInfo.outputPath("loaded-grid-photos.png"),
    });
  } finally {
    releaseImages();
  }
});

test("overscan photos prefetch just outside the viewport and explicitly decode", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1470, height: 833 });
  await page.addInitScript(() => {
    const decode = HTMLImageElement.prototype.decode;
    HTMLImageElement.prototype.decode = function () {
      this.dataset.decodeAttempted = "true";
      return decode.call(this);
    };
  });
  await stubFrontPage(
    page,
    [],
    Array.from({ length: 48 }, (_, index) =>
      item(`viewport-${index}`, "feed", `Photo ${index}`, 0.9, "L"),
    ),
    [],
  );
  await page.goto("/");
  const grid = page.locator(".grid-scroll");
  const first = page.locator('[data-item-id="viewport-0"] > img');
  await expect
    .poll(() =>
      first.evaluate(
        (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
      ),
    )
    .toBe(true);
  await expect(first).toHaveAttribute("data-decode-attempted", "true");
  const bufferedIDs = () =>
    grid.evaluate((element) => {
      const bottom = element.getBoundingClientRect().bottom;
      const margin = Math.min(240, element.clientHeight / 4);
      return Array.from(element.querySelectorAll(".grid-cell > img"))
        .filter((image) => {
          const top = image.getBoundingClientRect().top;
          return top >= bottom && top < bottom + margin;
        })
        .map((image) => image.parentElement?.getAttribute("data-item-id"));
    });
  await expect
    .poll(async () => (await bufferedIDs()).length)
    .toBeGreaterThan(0);
  const targetID = (await bufferedIDs())[0];
  const target = page.locator(`[data-item-id="${targetID}"] > img`);
  // Fetch ahead within the existing mounted buffer, before scrolling to it.
  await expect(target).toHaveAttribute("src", "/sema-mark.svg");
  await expect
    .poll(() =>
      target.evaluate(
        (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
      ),
    )
    .toBe(true);
  await expect(target).toHaveAttribute("data-decode-attempted", "true");
});

for (const width of [1470, 390]) {
  test(`image-free grid preserves layout and suppresses media requests at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 833 });
    const requests: string[] = [];
    await page.route("**/memory-experiment/**", async (route) => {
      requests.push(route.request().url());
      await route.fulfill({
        contentType: "image/svg+xml",
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="640" height="480"><rect width="640" height="480" fill="teal"/></svg>',
      });
    });
    const photo = (index: number) => ({
      ...item(`no-images-${index}`, "feed", `Photo ${index}`, 0.9, "L"),
      media_url: `/memory-experiment/photo-${index}.svg`,
      media_w: 640,
      media_h: 480,
      media_variants: [
        {
          url: `/memory-experiment/variant-${index}.svg`,
          width: 640,
          height: 480,
        },
      ],
      favicon_url: "/memory-experiment/favicon.svg",
    });
    await stubFrontPage(
      page,
      [
        {
          story_id: "no-images-story",
          source_count: 3,
          order_key: 0.95,
          size: "L",
          items: [photo(100), photo(101), photo(102)],
        },
      ],
      Array.from({ length: 48 }, (_, index) => photo(index)),
      [],
    );
    const grid = page.locator(".grid-scroll");
    const geometry = () =>
      grid.evaluate((element) => ({
        height: element.scrollHeight,
        cells: Array.from(element.querySelectorAll(".grid-cell")).map(
          (cell) => {
            const rect = (node: Element) => {
              const r = node.getBoundingClientRect();
              return [r.x, r.y, r.width, r.height].map(
                (v) => Math.round(v * 10) / 10,
              );
            };
            return {
              id: cell.getAttribute("data-item-id"),
              rect: rect(cell),
              images: Array.from(cell.querySelectorAll("img")).map(rect),
            };
          },
        ),
      }));
    await page.goto("/");
    await expect(grid).toHaveAttribute("data-grid-images", "on");
    await expect.poll(() => requests.length).toBeGreaterThan(0);
    await expect
      .poll(() =>
        grid
          .locator("img[src]")
          .evaluateAll((images) =>
            images.every((image) => (image as HTMLImageElement).complete),
          ),
      )
      .toBe(true);
    // Wait for image loading and story measurements to finish before comparison.
    await grid.evaluate(
      () =>
        new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        ),
    );
    const normal = await geometry();
    requests.length = 0;
    await page.goto("/?grid-images=off");
    await expect(grid).toHaveAttribute("data-grid-images", "off");
    await expect.poll(geometry).toEqual(normal);
    await expect(grid.locator("img")).not.toHaveCount(0);
    await expect(grid.locator("img[src], img[srcset]")).toHaveCount(0);
    expect(requests).toEqual([]);
    await page.keyboard.press("PageDown");
    await expect
      .poll(() => grid.evaluate((element) => element.scrollTop))
      .toBeGreaterThan(0);
    await grid.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect(page.locator('[data-item-id="no-images-47"]')).toBeVisible();
    await expect(grid.locator("img[src], img[srcset]")).toHaveCount(0);
    expect(requests).toEqual([]);
    await grid.evaluate((element) => {
      element.scrollTop = 0;
    });
    await expect(page.locator('[data-item-id="no-images-0"]')).toBeVisible();
    await page.locator('[data-item-id="no-images-0"] .cell-main').click();
    await expect(page.locator(".reader .article-lead")).toHaveAttribute(
      "src",
      "/memory-experiment/photo-0.svg",
    );
    await expect.poll(() => requests.length).toBeGreaterThan(0);
    requests.length = 0;
    await page.goto("/");
    await expect(grid).toHaveAttribute("data-grid-images", "on");
    await expect.poll(() => requests.length).toBeGreaterThan(0);
  });
}

test("grid releases detached nodes after repeated scrolling through loaded items", async ({
  page,
  browserName,
}) => {
  test.skip(browserName !== "chromium", "DOM counters require Chromium's CDP");
  test.setTimeout(90_000);
  await page.setViewportSize({ width: 1470, height: 833 });
  await stubFrontPage(
    page,
    [],
    Array.from({ length: 400 }, (_, index) => ({
      ...item(`retention-${index}`, "feed", `Photo ${index}`, 0.9, "L"),
      // Plain-src fixtures miss retention through native responsive listeners.
      media_variants: [
        { url: `/sema-mark.svg?small=${index}`, width: 768, height: 510 },
        { url: `/sema-mark.svg?large=${index}`, width: 1280, height: 850 },
      ],
    })),
    [],
  );
  await page.goto("/");
  const session = await page.context().newCDPSession(page);
  const grid = page.locator(".grid-scroll");
  const roundTrip = () =>
    grid.evaluate(async (element) => {
      const bottom = element.scrollHeight - element.clientHeight;
      const step = element.clientHeight;
      const visit = async (top: number) => {
        element.scrollTop = top;
        // Let virtual rows, intersection callbacks, and cached loads settle.
        await new Promise<void>((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
        );
      };
      for (let top = 0; top < bottom; top += step) await visit(top);
      await visit(bottom);
      for (let top = bottom; top > 0; top -= step) await visit(top);
      await visit(0);
    });
  const sample = async () => {
    await expect(
      page.locator('[data-item-id="retention-0"] > img'),
    ).toBeVisible();
    await page.evaluate(async () => {
      await document.fonts.ready;
      await Promise.all(
        Array.from(
          document.querySelectorAll<HTMLImageElement>(".grid-cell > img[src]"),
        ).map((image) => image.decode().catch(() => undefined)),
      );
      await new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      );
    });
    await session.send("HeapProfiler.collectGarbage");
    // Image decode completion and finalizers can release nodes on the next task.
    await page.evaluate(
      () => new Promise<void>((resolve) => setTimeout(resolve, 0)),
    );
    await session.send("HeapProfiler.collectGarbage");
    return {
      ...(await session.send("Memory.getDOMCounters")),
      ...(await session.send("Runtime.getHeapUsage")),
    };
  };
  const initial = await sample();
  await roundTrip();
  const baseline = await sample();
  await roundTrip();
  const after = await sample();
  console.log("Grid retention after warm-up and another round trip", {
    initial,
    baseline,
    after,
  });
  // Count detached as well as connected nodes. Allow small browser bookkeeping
  // differences, but not another viewport's worth of retained cards.
  expect(after.nodes).toBeLessThan(initial.nodes + 400);
  expect(after.nodes).toBeLessThan(baseline.nodes + 200);
  expect(after.jsEventListeners).toBeLessThan(baseline.jsEventListeners + 20);
  await session.detach();
});

test("grid and reader both use the decode workaround", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.addInitScript(() => {
    HTMLImageElement.prototype.decode = function () {
      this.dataset.decodeAttempted = "true";
      return Promise.reject(
        new DOMException("Source changed", "EncodingError"),
      );
    };
  });
  await stubFrontPage(
    page,
    [],
    [item("decode-error", "feed", "Decode failure", 0.9, "L")],
    [],
  );
  await page.goto("/");
  const card = page.locator('[data-item-id="decode-error"]');
  await expect
    .poll(() =>
      card
        .locator(":scope > img")
        .evaluate(
          (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
        ),
    )
    .toBe(true);
  await expect(card.locator(":scope > img")).toHaveAttribute(
    "data-decode-attempted",
    "true",
  );
  await card.locator(".cell-main").click();
  await expect(page.locator(".reader")).toBeVisible();
  await expect(page.locator(".reader .article-lead")).toHaveAttribute(
    "data-decode-attempted",
    "true",
  );
  expect(errors).toEqual([]);
});

test("story footers and neighboring singletons share the same bottom edge", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1513, height: 1071 });
  await page.addInitScript(() => localStorage.setItem("sema:theme", "light"));
  const story = {
    story_id: "aligned-story",
    source_count: 6,
    order_key: 0.85,
    size: "L",
    items: [
      {
        ...item(
          "aligned-lead",
          "ign",
          "Until Dawn 2 Reveals Release Date in New Trailer",
          0.85,
          "L",
        ),
        summary: "",
      },
      ...Array.from({ length: 5 }, (_, index) =>
        item(
          `aligned-related-${index}`,
          "overworld",
          `Other coverage ${index}: the January release date and new trailer reveal more details`,
          0.7,
          "S",
        ),
      ),
    ],
  };
  const items = Array.from({ length: 8 }, (_, index) => ({
    ...item(
      `aligned-singleton-${index}`,
      "feed",
      `Singleton ${index} with a headline below its photo`,
      0.9 - index * 0.1,
      "L",
    ),
    summary: "",
  }));
  await stubFrontPage(page, [story], items, []);
  await page.goto("/");
  const card = page.locator('[data-story-id="aligned-story"]');
  const row = page.locator(".grid-row").filter({ has: card });
  const expectAligned = async () => {
    await expect(card).toBeVisible();
    await expect
      .poll(() =>
        row.locator(".grid-cell").evaluateAll((cells) => {
          const bounds = cells.map((cell) => cell.getBoundingClientRect());
          return (
            bounds.length > 1 &&
            bounds.every(
              (rect) =>
                Math.abs(rect.top - bounds[0].top) < 1 &&
                Math.abs(rect.bottom - bounds[0].bottom) < 1,
            )
          );
        }),
      )
      .toBe(true);
  };
  for (const width of [1513, 1024, 768]) {
    await page.setViewportSize({ width, height: 1071 });
    await expectAligned();
  }
  await page.setViewportSize({ width: 1513, height: 1071 });
  await expectAligned();
  await page.screenshot({ path: "/tmp/sema-story-row-aligned.png" });
  await card.getByRole("button", { name: /\+\d+ more/ }).click();
  await expect(card.locator(".story-headline")).toHaveCount(5);
  await expectAligned();
  await expect
    .poll(() =>
      page.locator(".grid-row").evaluateAll((rows) => {
        const bounds = rows
          .map((row) => row.getBoundingClientRect())
          .sort((a, b) => a.top - b.top);
        return bounds.every(
          (rect, index) => index === 0 || rect.top > bounds[index - 1].bottom,
        );
      }),
    )
    .toBe(true);
});

for (const preference of ["dark", "system"] as const) {
  test(`dark grid uses established tokens and shared read states (${preference})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1513, height: 1071 });
    await page.emulateMedia({
      colorScheme: preference === "system" ? "dark" : "light",
    });
    await page.addInitScript(
      (theme) => localStorage.setItem("sema:theme", theme),
      preference,
    );
    const lead = {
      ...item(
        "dark-lead",
        "hn",
        "Smartphone makers don't bother to comply with EU repairability requirements",
        0.95,
        "L",
      ),
      feed_title: "Hacker News: Front Page",
      media_url: "/dark-grid-fixture.svg",
      summary: "",
    };
    const story = {
      story_id: "dark-story",
      source_count: 3,
      order_key: 0.95,
      size: "L",
      items: [
        lead,
        {
          ...item("dark-duplicate", "register", lead.title, 0.7, "S"),
          feed_title: "www.theregister.com - Articles",
        },
        item(
          "dark-angle",
          "ars",
          "Why repairability requirements matter to consumers",
          0.6,
          "S",
        ),
      ],
    };
    const readLead = {
      ...item(
        "dark-read-lead",
        "vancouver",
        "Railtown: Rethinking Vancouver's industrial-creative enclave",
        0.85,
        "L",
      ),
      feed_title: "r/vancouver",
      read: true,
      media_url: "/dark-grid-fixture.svg",
      summary: "",
    };
    const readStory = {
      story_id: "dark-read-story",
      source_count: 2,
      order_key: 0.85,
      size: "L",
      items: [
        readLead,
        {
          ...item(
            "dark-read-related",
            "sun",
            "Vancouver&#39;s industrial future",
            0.5,
            "S",
          ),
          read: true,
        },
      ],
    };
    const items = [
      {
        ...item(
          "dark-single",
          "ars",
          "The complex corporate web behind a $3.2 billion AI data center",
          1,
          "L",
        ),
        summary: "",
        media_url: "/dark-grid-fixture.svg",
      },
      {
        ...item(
          "dark-read-single",
          "9to5",
          "Reports describe two ways for Apple to make more money: only one is good",
          0.9,
          "L",
        ),
        read: true,
        summary: "",
        media_url: "/dark-grid-fixture.svg",
      },
      {
        ...item(
          "dark-low",
          "ign",
          "Until Dawn 2 Reveals Release Date in New Trailer",
          0.8,
          "L",
        ),
        summary: "",
        media_url: "/dark-grid-fixture.svg",
      },
      {
        ...item(
          "dark-last",
          "lobsters",
          "Conquering Entropy: Cultivating Trust",
          0.75,
          "L",
        ),
        summary: "",
        media_url: "/dark-grid-fixture.svg",
      },
    ];
    await page.route("**/dark-grid-fixture.svg", (route) =>
      route.fulfill({
        contentType: "image/svg+xml",
        body: '<svg xmlns="http://www.w3.org/2000/svg" width="600" height="300"><rect width="600" height="300" fill="#50696e"/><path d="M0 250L140 40L360 220L530 70L600 140V300H0Z" fill="#91aaab"/></svg>',
      }),
    );
    await stubFrontPage(page, [story, readStory], items, []);
    await page.goto("/");
    await page.getByRole("radio", { name: "All", exact: true }).click();
    const grid = page.locator(".grid-scroll");
    const card = page.locator('[data-story-id="dark-story"]');
    const readCard = page.locator('[data-story-id="dark-read-story"]');
    const singleton = page.locator('[data-item-id="dark-single"]');
    const readSingleton = page.locator('[data-item-id="dark-read-single"]');
    await expect(grid).toHaveClass(/refined-grid/);
    await expect(page.locator(".app-header")).toHaveCSS(
      "background-color",
      "rgb(11, 12, 14)",
    );
    for (const cell of [card, readCard, singleton, readSingleton]) {
      await expect(cell).toHaveCSS("background-color", "rgb(23, 24, 26)");
      await expect(cell).toHaveCSS("border-top-color", "rgb(28, 29, 31)");
      await expect(cell.locator("h2").first()).toHaveCSS(
        "color",
        "rgb(244, 242, 238)",
      );
      await expect(cell).toHaveCSS("opacity", "1");
    }
    await expect(readCard.locator(".story-media")).toHaveCSS("filter", "none");
    await expect(readSingleton.locator(":scope > img")).toHaveCSS(
      "filter",
      "none",
    );
    await expect(card.locator(".story-badges")).toBeHidden();
    await expect(card.locator(".story-corner")).toBeHidden();
    await expect(card.locator(".story-headlines")).toHaveCSS(
      "background-color",
      "rgb(16, 17, 19)",
    );
    await expect(
      card
        .locator(".story-meta > span:not(.source-badge):not(.unread-dot)")
        .first(),
    ).toHaveCSS("color", "rgb(168, 174, 182)");
    await expect(card.locator(".story-meta .unread-dot")).toHaveCSS(
      "box-shadow",
      "none",
    );
    await expect(card.locator(".story-meta .unread-dot")).toHaveCSS(
      "background-color",
      "rgb(214, 242, 75)",
    );
    await expect(readCard.locator(".story-meta .unread-dot")).toHaveCount(0);
    await expect(card.locator(".story-headline").first()).toContainText(
      "Also covered by The Register",
    );
    await singleton.hover();
    await expect(singleton).toHaveCSS("background-color", "rgb(31, 33, 36)");
    await expect(singleton).toHaveCSS("outline-style", "none");
    const footer = card.locator(".story-headline").first();
    await footer.hover();
    await expect(footer).toHaveCSS("background-color", "rgb(31, 33, 36)");
    await card.locator(".story-lead").focus();
    await page.keyboard.press("ArrowDown");
    await expect(footer).toBeFocused();
    await expect(footer).toHaveCSS("outline-color", "rgb(214, 242, 75)");
    await page.keyboard.press("ArrowUp");
    await expect(card.locator(".story-lead")).toBeFocused();
    await expect(card).toHaveCSS("outline-color", "rgb(214, 242, 75)");
    await expect(card.locator(".ranking-hint")).toHaveCSS("opacity", "1");
    await page.locator(".app-header").hover();
    await page.screenshot({
      path: `/tmp/sema-direction-a-dark-${preference}.png`,
    });
    for (const width of [1024, 768]) {
      await page.setViewportSize({ width, height: 1071 });
      await expect(card.locator(".story-media-action")).toHaveCSS(
        "height",
        "126px",
      );
      await expect
        .poll(() =>
          singleton
            .locator(":scope > img")
            .evaluate((img) => img.getBoundingClientRect().height),
        )
        .toBeGreaterThan(126);
    }
    await page.setViewportSize({ width: 1513, height: 1071 });
    await page.getByRole("radio", { name: "Unread", exact: true }).click();
    await expect(grid.locator(".unread-dot")).toHaveCount(0);
    await expect(readCard).toHaveCSS("background-color", "rgb(16, 17, 19)");
    await expect(readCard.locator(".story-headlines")).toHaveCSS(
      "background-color",
      "rgb(11, 12, 14)",
    );
    await expect(readCard.locator(".story-lead h2")).toHaveCSS(
      "color",
      "rgb(168, 174, 182)",
    );
    await expect(readCard.locator(".story-media")).toHaveCSS(
      "filter",
      "opacity(0.55)",
    );
    await singleton.hover();
    await page.keyboard.press("m");
    await page.locator(".app-header").hover();
    await expect(singleton.locator(":scope > img")).toHaveCSS(
      "filter",
      "opacity(0.55)",
    );
    await expect(singleton.locator("h2")).toHaveCSS(
      "color",
      "rgb(168, 174, 182)",
    );
    await page.screenshot({
      path: `/tmp/sema-direction-a-dark-unread-${preference}.png`,
    });
  });
}

test.describe("mobile shared grid design", () => {
  test.use({ hasTouch: true, isMobile: true });
  for (const theme of ["light", "dark"] as const) {
    for (const width of [320, 393, 430]) {
      test(`fits solid cards and related coverage at ${width}px in ${theme}`, async ({
        page,
      }) => {
        await page.setViewportSize({ width, height: 920 });
        await page.addInitScript(
          (theme) => localStorage.setItem("sema:theme", theme),
          theme,
        );
        await page.route("**/mobile-landscape.svg", (route) =>
          route.fulfill({
            contentType: "image/svg+xml",
            body: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 360"><path fill="#50696e" d="M0 0h640v360H0z"/><path fill="#91a9a9" d="m0 300 180-240 220 230L560 100l80 80v180H0z"/></svg>',
          }),
        );
        const photo = {
          media_url: "/mobile-landscape.svg",
          media_w: 640,
          media_h: 360,
        };
        const lead = {
          ...item(
            "phone-lead",
            "hn",
            "Smartphone makers don't bother to comply with EU repairability requirements",
            1,
            "L",
          ),
          ...photo,
          feed_title: "Hacker News: Front Page",
        };
        const duplicate = {
          ...item("phone-duplicate", "register", lead.title, 0.8, "S"),
          feed_title: "www.theregister.com - Articles",
        };
        const story = {
          story_id: "phone-story",
          source_count: 3,
          order_key: 1,
          size: "L",
          items: [
            lead,
            duplicate,
            item(
              "phone-angle",
              "ars",
              "What the new repairability rules mean for consumers and the devices they own",
              0.7,
              "S",
            ),
          ],
        };
        const readLead = {
          ...item(
            "phone-read",
            "ign",
            "Until Dawn 2 Reveals Release Date in New Trailer",
            0.95,
            "L",
          ),
          ...photo,
          feed_title: "IGN All",
          read: true,
        };
        const readStory = {
          story_id: "phone-read-story",
          source_count: 2,
          order_key: 0.95,
          size: "L",
          items: [
            readLead,
            {
              ...item(
                "phone-read-duplicate",
                "eurogamer",
                readLead.title,
                0.6,
                "S",
              ),
              feed_title: "Eurogamer.net Latest Articles Feed",
              read: true,
            },
          ],
        };
        const items = [
          {
            ...item(
              "phone-single",
              "polygon",
              "Apple Maps changes name of Lake Ontario to Lake America",
              0.9,
              "L",
            ),
            ...photo,
            feed_title: "Polygon.com",
          },
          {
            ...item(
              "phone-text",
              "custom",
              "A thoughtful look at Vancouver's changing neighbourhoods",
              0.5,
              "M",
            ),
            feed_title: "A custom community name that should be preserved",
          },
          {
            ...item(
              "phone-medium",
              "news",
              "How a new generation of batteries could change electric vehicles",
              0.4,
              "M",
            ),
            ...photo,
          },
          ...[1, 2, 3].map((n) => ({
            ...item(
              `phone-small-${n}`,
              "hn",
              "Developers discuss a new approach to software performance",
              0.3 - n * 0.01,
              "S",
            ),
            ...photo,
            feed_title: "Hacker News: Front Page",
          })),
        ];
        await stubFrontPage(page, [story, readStory], items, []);
        await page.goto("/");
        const grid = page.locator(".grid-scroll");
        const card = page.locator('[data-story-id="phone-story"]');
        const readCard = page.locator('[data-story-id="phone-read-story"]');
        const single = page.locator('[data-item-id="phone-single"]');
        await expect(grid).toHaveClass(/mobile-refined-grid/);
        await expect(grid.locator(".unread-dot")).toHaveCount(0);
        await expect(card.locator(".story-related-source")).toHaveText(
          "Also covered by The Register · 1d",
        );
        await expect(card.locator(".story-meta")).toContainText("Hacker News");
        await expect(card.locator(".story-headlines")).toHaveCSS(
          "background-color",
          theme === "light" ? "rgb(250, 249, 245)" : "rgb(16, 17, 19)",
        );
        await expect(readCard).toHaveClass(/is-read/);
        await expect(readCard.locator(":scope > img")).toHaveCSS(
          "filter",
          "opacity(0.55)",
        );
        await expect(single).toHaveCSS(
          "background-color",
          theme === "light" ? "rgb(255, 255, 255)" : "rgb(23, 24, 26)",
        );
        await page.evaluate(() => document.fonts.ready);
        await expect
          .poll(() =>
            grid.evaluate((el) => {
              const problems: string[] = [];
              for (const card of el.querySelectorAll<HTMLElement>(
                ".grid-cell",
              )) {
                const box = card.getBoundingClientRect();
                for (const copy of card.querySelectorAll<HTMLElement>(
                  "h2, .cell-meta, .story-meta, .story-related-copy",
                )) {
                  const r = copy.getBoundingClientRect();
                  if (r.width === 0) continue;
                  if (
                    r.bottom > box.bottom + 1 ||
                    r.right > box.right + 1 ||
                    r.left < box.left - 1
                  )
                    problems.push(
                      `${card.dataset.itemId}: ${copy.className || copy.tagName}`,
                    );
                }
                const img = card.querySelector<HTMLElement>(":scope > img");
                const title = card.querySelector(".cell-copy h2");
                if (
                  img &&
                  title &&
                  img.getBoundingClientRect().bottom >
                    title.getBoundingClientRect().top
                )
                  problems.push("image overlaps title");
                if (
                  img &&
                  !card.classList.contains("size-s") &&
                  !card.classList.contains("size-m") &&
                  img.getBoundingClientRect().height < 48
                )
                  problems.push("image too short");
              }
              return problems;
            }),
          )
          .toEqual([]);
        await card.getByRole("button", { name: "+1 more" }).tap();
        await expect(card.locator(".story-related-title")).toHaveText(
          "What the new repairability rules mean for consumers and the devices they own",
        );
        await expect
          .poll(() =>
            card.evaluate((el) => {
              const meta = el
                .querySelector(".story-meta")
                ?.getBoundingClientRect();
              const footer = el
                .querySelector(".story-headlines")
                ?.getBoundingClientRect();
              const copy = el
                .querySelector(".story-related-title")
                ?.getBoundingClientRect();
              return (
                !!meta &&
                !!footer &&
                !!copy &&
                meta.bottom <= footer.top &&
                copy.bottom <= el.getBoundingClientRect().bottom
              );
            }),
          )
          .toBe(true);
        await card.getByRole("button", { name: "Show less" }).tap();
        await page
          .getByRole("button", { name: "Front page", exact: true })
          .tap();
        await page.getByRole("radio", { name: "All", exact: true }).tap();
        await page.getByRole("button", { name: "Close feed view menu" }).tap();
        await expect(readCard).not.toHaveClass(/is-read/);
        await expect(readCard.locator(":scope > img")).toHaveCSS(
          "filter",
          "none",
        );
        await expect(readCard.locator("h2")).toHaveCSS(
          "color",
          theme === "light" ? "rgb(20, 22, 26)" : "rgb(244, 242, 238)",
        );
        await expect(single.locator(".cell-meta .unread-dot")).toBeVisible();
        await expect(readCard.locator(".unread-dot")).toHaveCount(0);
        await expect(single.locator(".cell-feed-filter")).toHaveText("Polygon");
        await expect(
          single.locator(".refined-age .age-separator"),
        ).toBeHidden();
        await expect
          .poll(() =>
            grid
              .locator(".grid-cell > img")
              .evaluateAll((images) =>
                images.every((img) => getComputedStyle(img).opacity === "1"),
              ),
          )
          .toBe(true);
        await expect(page.locator('[data-item-id="phone-text"] h2')).toHaveCSS(
          "padding-right",
          "0px",
        );
        await page.screenshot({
          path: `/tmp/sema-mobile-${theme}-${width}.png`,
        });
        await readCard
          .getByRole("button", { name: "2 sources, show headlines" })
          .tap();
        const sheet = page.getByRole("dialog", {
          name: `Actions for ${readLead.title}`,
        });
        await expect(sheet).toBeVisible();
        await expect(sheet.locator(".sheet-headline")).not.toHaveClass(/read/);
        await expect(sheet.locator(".story-related-source")).toHaveText(
          "Also covered by Eurogamer · 1d",
        );
        await sheet
          .getByRole("button", { name: `Open ${readLead.title}`, exact: true })
          .tap();
        await expect(page.locator(".reader-scroll")).toBeVisible();
      });
    }
  }
});

test.describe("grid thumbnail budget", () => {
  test.use({ viewport: { width: 1470, height: 833 }, deviceScaleFactor: 2 });

  test("Retina grid selects a small variant while the reader keeps the large image", async ({
    page,
  }) => {
    const requests: string[] = [];
    await page.route("**/thumbnail-budget/**", async (route) => {
      requests.push(route.request().url());
      const width = route.request().url().includes("small") ? 768 : 1280;
      await route.fulfill({
        contentType: "image/svg+xml",
        body: `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${width / 2}"><rect width="100%" height="100%" fill="teal"/></svg>`,
      });
    });
    const photo = {
      ...item("thumbnail-budget", "feed", "Thumbnail budget", 0.9, "L"),
      media_url: "/thumbnail-budget/large.svg",
      media_w: 1280,
      media_h: 640,
      media_variants: [
        { url: "/thumbnail-budget/small.svg", width: 768, height: 384 },
        { url: "/thumbnail-budget/large.svg", width: 1280, height: 640 },
      ],
    };
    await stubFrontPage(page, [], [photo], []);
    await page.goto("/");
    const image = page.locator('[data-item-id="thumbnail-budget"] img').first();
    await expect
      .poll(() =>
        image.evaluate((node) => (node as HTMLImageElement).currentSrc),
      )
      .toContain("/thumbnail-budget/small.svg");
    await expect(page.locator(".grid-scroll img[srcset]")).toHaveCount(0);
    expect(requests.some((url) => url.endsWith("/large.svg"))).toBe(false);
    await page.locator('[data-item-id="thumbnail-budget"] .cell-main').click();
    const lead = page.locator(".reader .article-lead");
    await expect
      .poll(() =>
        lead.evaluate((node) => (node as HTMLImageElement).currentSrc),
      )
      .toContain("/thumbnail-budget/large.svg");
  });
});

for (const theme of ["dark", "light", "system"] as const) {
  test(`feedback controls follow the ${theme} theme`, async ({ page }) => {
    const light = theme !== "dark";
    const rest = light ? "rgb(244, 241, 234)" : "rgba(6, 7, 9, 0.72)";
    const secondary = light ? "rgb(61, 67, 75)" : "rgb(199, 204, 211)";
    const selected = light ? "rgb(224, 220, 210)" : "rgb(42, 45, 49)";
    const hover = light ? selected : "rgb(31, 33, 36)";
    const primary = light ? "rgb(20, 22, 26)" : "rgb(244, 242, 238)";
    const fixtures = [
      { ...item("boosted", "one", "Boosted item", 0.9, "M"), signal: 1 },
      { ...item("buried", "one", "Buried image", 0.8, "L"), signal: -1 },
      { ...item("buried-text", "one", "Buried text", 0.7, "M"), signal: -1 },
      {
        ...item("kept", "one", "Kept boosted item", 0.6, "M"),
        signal: 1,
        hearted: true,
      },
    ];
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
    await page.addInitScript(
      (theme) => localStorage.setItem("sema:theme", theme),
      theme,
    );
    await stubFrontPage(page, [], fixtures, []);
    for (const fixture of fixtures) {
      await page.route(`**/api/items/${fixture.item_id}`, (route) =>
        route.fulfill({ json: fixture }),
      );
    }
    await page.goto("/");
    for (const fixture of fixtures) {
      const cell = page.locator(`[data-item-id="${fixture.item_id}"]`);
      await cell.hover();
      const label = cell.locator(".cell-signal-label");
      await expect(label).toHaveCSS(
        "background-color",
        await cell
          .locator(".cell-age")
          .evaluate((el) => getComputedStyle(el).backgroundColor),
      );
      await expect(label).toHaveCSS(
        "color",
        fixture.signal === 1
          ? light
            ? "rgb(95, 122, 12)"
            : "rgb(214, 242, 75)"
          : await cell
              .locator(".cell-age")
              .evaluate((el) => getComputedStyle(el).color),
      );
      const more = cell.locator('button[data-action="more"]');
      await expect(more).toHaveCSS("background-color", rest);
      await expect(more.locator(".icon")).toHaveCSS("color", secondary);
      for (const action of ["boost", "bury", "keep", "more"]) {
        const button = cell.locator(`button[data-action="${action}"]`);
        await button.hover();
        const pressed = (await button.getAttribute("aria-pressed")) === "true";
        const disabled = await button.isDisabled();
        const background = disabled
          ? rest
          : pressed
            ? action === "boost"
              ? "rgb(214, 242, 75)"
              : selected
            : hover;
        const foreground = disabled
          ? secondary
          : pressed && action === "boost"
            ? "rgb(18, 22, 10)"
            : primary;
        await expect(button).toHaveCSS("background-color", background);
        await expect(button).toHaveCSS("color", foreground);
        await expect(button.locator(".icon")).toHaveCSS("color", foreground);
        if (disabled) await expect(button).toHaveCSS("opacity", "0.4");
      }
    }
    await page.screenshot({ path: `/tmp/sema-feedback-theme-${theme}.png` });
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 1000 });
      for (const [id, action, background, foreground] of [
        ["boosted", "boost", "rgb(214, 242, 75)", "rgb(18, 22, 10)"],
        ["buried", "bury", selected, primary],
      ]) {
        const cell = page.locator(`[data-item-id="${id}"]`);
        if (width === 390) {
          const chip = cell.locator(".signal-mobile-chip");
          await expect(chip).toBeVisible();
          await expect(chip).toHaveCSS("background-color", background);
          await expect(chip.locator(".icon")).toHaveCSS("color", foreground);
        }
        await cell.locator(".cell-main").click();
        const button = page.locator(
          `.reader [data-action="${action}"]:visible`,
        );
        await expect(button).toHaveCSS("background-color", background);
        await expect(button.locator(".icon")).toHaveCSS("color", foreground);
        await button.hover();
        await expect(button).toHaveCSS("background-color", background);
        await expect(button.locator(".icon")).toHaveCSS("color", foreground);
        await page.keyboard.press("Escape");
      }
    }
  });
}

for (const theme of ["dark", "light"] as const) {
  test(`time-left markers replace ages and stay on hover in ${theme}`, async ({
    page,
  }) => {
    const near = {
      ...item("near", "feed", "Near deadline", 0.7, "M"),
      published_ts: "2026-09-04T18:00:00Z",
    };
    const lead = {
      ...near,
      item_id: "lead",
      title: "Story deadline",
      size: "L",
    };
    await stubFrontPage(
      page,
      [
        {
          story_id: "deadline",
          source_count: 2,
          order_key: 0.99,
          size: "L",
          items: [lead, { ...near, item_id: "related" }],
        },
      ],
      [near, { ...near, item_id: "kept", hearted: true }],
      [],
    );
    await page.clock.install({ time: new Date("2026-09-10T23:00:00Z") });
    await page.addInitScript(
      (value) => localStorage.setItem("sema:theme", value),
      theme,
    );
    await page.goto("/");
    const cell = page.locator('[data-item-id="near"]');
    await expect(cell.locator(".expiry-pill")).toHaveText("19h left");
    await expect(cell.locator(".expiry-pill")).toBeVisible();
    await expect(cell.locator(".cell-corner .cell-age")).toHaveCount(0);
    await cell.hover();
    await expect(cell.locator(".expiry-pill")).toHaveCSS("opacity", "1");
    await expect(cell.locator(".cell-rank")).toHaveCSS("opacity", "1");
    await expect(
      page.locator('[data-story-id="deadline"] .expiry-pill'),
    ).toHaveText("19h left");
    await expect(
      page.locator('[data-item-id="kept"] .expiry-pill'),
    ).toHaveCount(0);
    await expect(
      cell.getByRole("button", { name: /Open Near deadline.*19 hours left/ }),
    ).toBeVisible();
    await page.screenshot({
      path: `e2e/screenshots/expiring-grid-${theme}.png`,
    });
    await page.clock.fastForward(14 * 60 * 60 * 1000);
    await expect(cell.locator(".expiry-pill")).toHaveText("5h left");
  });
}
