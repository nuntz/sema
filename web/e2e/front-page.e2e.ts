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
  await expect(storyCell.getByLabel("2 sources")).toBeVisible();
  await expect(storyCell.locator(".story-badges")).toHaveCount(0);
  await expect(storyCell.locator(".cell-rank")).toHaveCount(0);
  await expect(storyCell.locator(".story-headlines")).toHaveCount(0);
  await page.screenshot({ path: "/tmp/sema-medium-story.png" });

  await storyCell.hover();
  await storyCell.getByRole("button", { name: "More actions" }).click();
  const sheet = page.getByRole("dialog", {
    name: "Actions for Medium story lead",
  });
  await expect(sheet).toContainText("2 sources");
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
