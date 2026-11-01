import type { Page } from "@playwright/test";

// A believable front page for the README screenshot: invented Feeds and Items, with
// lead images drawn here as SVG so the repository owns every pixel it publishes.

type Scene =
  | "ridges"
  | "horizon"
  | "comet"
  | "poster"
  | "skyline"
  | "waves"
  | "dunes";

interface ShowcaseItem {
  id: string;
  feed: string;
  title: string;
  summary: string;
  hoursAgo: number;
  size: "S" | "M" | "L";
  score: number;
  scene?: Scene;
  palette?: string[];
  ratio?: number;
  read?: boolean;
  story?: string;
}

const feeds = [
  { id: "field-notes", title: "Field Notes", tags: ["outdoors"] },
  { id: "long-exposure", title: "The Long Exposure", tags: ["photography"] },
  { id: "kiln-cog", title: "Kiln & Cog", tags: ["making"] },
  { id: "low-orbit", title: "Low Orbit", tags: ["science"] },
  { id: "margins", title: "Margins Weekly", tags: ["design"] },
  { id: "small-engines", title: "Small Engines", tags: ["tech"] },
  { id: "pantry", title: "Pantry Letters", tags: ["food"] },
  { id: "tideline", title: "Tideline", tags: ["outdoors", "science"] },
];

const items: ShowcaseItem[] = [
  {
    id: "ridge-walk",
    feed: "field-notes",
    title: "Three days on the high ridge, and what the map left out",
    summary:
      "The route looked simple on paper. The weather, the scree and a missing spring had other ideas.",
    hoursAgo: 2,
    size: "L",
    score: 0.94,
    scene: "ridges",
    palette: ["#2f4858", "#4f6d7a", "#8aa1a8", "#c9d3cf", "#f2e3c6"],
    ratio: 1.6,
  },
  {
    id: "blue-hour",
    feed: "long-exposure",
    title: "Chasing blue hour with a forty-year-old lens",
    summary:
      "Old glass flares, softens and forgives. A week of dusk shooting with a manual prime.",
    hoursAgo: 5,
    size: "M",
    score: 0.81,
    scene: "horizon",
    palette: ["#1d2b4c", "#3b4f7d", "#7d84b2", "#e8a87c", "#f4d9b0"],
    ratio: 1.5,
  },
  {
    id: "type-specimen",
    feed: "margins",
    title: "Why every type specimen still looks like 1962",
    summary:
      "Grids, big numerals and a single red accent: the specimen book's long, stubborn style.",
    hoursAgo: 3,
    size: "M",
    score: 0.78,
    scene: "poster",
    palette: ["#f1ebe0", "#d64933", "#1f1f1f", "#e2b33c", "#2b59c3"],
    ratio: 0.8,
  },
  {
    id: "comet",
    feed: "low-orbit",
    title: "A comet is falling apart, and astronomers are thrilled",
    summary:
      "Fragments now trail the nucleus by thousands of kilometres, a rare look at what a comet is made of.",
    hoursAgo: 7,
    size: "M",
    score: 0.76,
    scene: "comet",
    palette: ["#0b1026", "#1b2a52", "#3e4c84", "#c8b6e2", "#fdf6e3"],
    ratio: 1.78,
    story: "comet",
  },
  {
    id: "comet-2",
    feed: "tideline",
    title: "How to see the breaking comet this weekend",
    summary: "Look low in the west an hour after sunset, binoculars ready.",
    hoursAgo: 6,
    size: "M",
    score: 0.7,
    scene: "horizon",
    palette: ["#101830", "#283a6a", "#5a6aa0", "#f0c987", "#fff4dc"],
    ratio: 1.78,
    story: "comet",
  },
  {
    id: "glaze",
    feed: "kiln-cog",
    title: "The glaze that took eleven firings to get right",
    summary: "Notes on ash, cobalt and patience from a small studio kiln.",
    hoursAgo: 9,
    size: "M",
    score: 0.72,
    scene: "waves",
    palette: ["#16324f", "#2a628f", "#5e9fb8", "#a6d0d6", "#efe7da"],
    ratio: 1.25,
  },
  {
    id: "compiler",
    feed: "small-engines",
    title:
      "Writing a tiny compiler in a weekend, then living with it for a year",
    summary:
      "The parser took a day. The error messages took the next eleven months.",
    hoursAgo: 4,
    size: "S",
    score: 0.66,
  },
  {
    id: "sourdough",
    feed: "pantry",
    title: "A slower sourdough, and why the fridge is your friend",
    summary:
      "Cold proofing for two days changes everything about crumb and crust.",
    hoursAgo: 11,
    size: "S",
    score: 0.61,
    scene: "dunes",
    palette: ["#5b3a29", "#a0673f", "#d9a066", "#f0d3a4", "#fbf3e4"],
    ratio: 1.5,
  },
  {
    id: "city-grid",
    feed: "margins",
    title: "The city that redrew its street signs from scratch",
    summary:
      "A new wayfinding system, tested on commuters before a single sign went up.",
    hoursAgo: 14,
    size: "S",
    score: 0.58,
    scene: "skyline",
    palette: ["#f4e9d8", "#e07a5f", "#3d405b", "#81b29a", "#f2cc8f"],
    ratio: 1.5,
  },
  {
    id: "tide-pools",
    feed: "tideline",
    title: "Life in the tide pools after the winter storms",
    summary: "Anemones are back first; the sea stars are taking longer.",
    hoursAgo: 16,
    size: "S",
    score: 0.55,
    scene: "waves",
    palette: ["#0f3d3e", "#1f6f6a", "#56a39a", "#a8d5ba", "#f1f7ed"],
    ratio: 1.33,
  },
  {
    id: "keyboards",
    feed: "small-engines",
    title: "In defence of boring keyboards",
    summary:
      "After a decade of custom switches, a return to the laptop's flat keys.",
    hoursAgo: 18,
    size: "S",
    score: 0.52,
  },
  {
    id: "desert-road",
    feed: "long-exposure",
    title: "Empty roads: a desert series shot from the driver's seat",
    summary: "Two thousand kilometres, one window and very little traffic.",
    hoursAgo: 20,
    size: "S",
    score: 0.5,
    scene: "dunes",
    palette: ["#3c2a4d", "#9e4770", "#e27d60", "#f5b971", "#fde8c8"],
    ratio: 1.78,
  },
  {
    id: "bike-shed",
    feed: "kiln-cog",
    title: "Building a bike shed with no right angles",
    summary:
      "A small timber structure, a lot of scrap offcuts and one very patient neighbour.",
    hoursAgo: 23,
    size: "S",
    score: 0.47,
    scene: "poster",
    palette: ["#e9e4d8", "#33658a", "#2f4858", "#f6ae2d", "#f26419"],
    ratio: 1.2,
  },
  {
    id: "moss",
    feed: "field-notes",
    title: "A beginner's guide to telling mosses apart",
    summary: "A hand lens, a damp log and twenty minutes is all it takes.",
    hoursAgo: 26,
    size: "S",
    score: 0.44,
  },
  {
    id: "orbit",
    feed: "low-orbit",
    title: "What it takes to park a telescope a million miles away",
    summary: "Station keeping, sunshields and a very small fuel budget.",
    hoursAgo: 29,
    size: "S",
    score: 0.42,
    scene: "ridges",
    palette: ["#0d1b2a", "#1b263b", "#415a77", "#778da9", "#e0e1dd"],
    ratio: 1.6,
  },
  {
    id: "spice",
    feed: "pantry",
    title: "Toasting spices: the thirty seconds that matter",
    summary: "Heat, smell, and when to pull the pan.",
    hoursAgo: 31,
    size: "S",
    score: 0.4,
    read: true,
  },
  {
    id: "ferry",
    feed: "field-notes",
    title: "The last ferry out, and the island after dark",
    summary: "Missing the boat turned into the best night of the trip.",
    hoursAgo: 34,
    size: "S",
    score: 0.38,
    scene: "horizon",
    palette: ["#22223b", "#4a4e69", "#9a8c98", "#f2a65a", "#f2e9e4"],
    ratio: 1.5,
  },
  {
    id: "poster-archive",
    feed: "margins",
    title: "Inside a library of ten thousand gig posters",
    summary: "Screen prints, staples and decades of local music history.",
    hoursAgo: 38,
    size: "S",
    score: 0.35,
    scene: "poster",
    palette: ["#1b1b1e", "#f4d35e", "#ee964b", "#f95738", "#faf0ca"],
    ratio: 0.75,
  },
  {
    id: "night-trains",
    feed: "field-notes",
    title: "Sleeper trains are back, and so is the dining car",
    summary: "Overnight routes are returning across the continent.",
    hoursAgo: 40,
    size: "S",
    score: 0.33,
    scene: "skyline",
    palette: ["#1d3557", "#457b9d", "#a8dadc", "#2b2d42", "#f1faee"],
    ratio: 1.5,
  },
  {
    id: "mushrooms",
    feed: "pantry",
    title: "Growing oyster mushrooms on coffee grounds",
    summary: "A bucket, a dark cupboard and a fortnight.",
    hoursAgo: 42,
    size: "S",
    score: 0.31,
  },
  {
    id: "aurora",
    feed: "low-orbit",
    title: "The aurora forecast is getting surprisingly good",
    summary: "Solar wind data now gives a useful few hours of warning.",
    hoursAgo: 44,
    size: "S",
    score: 0.3,
    scene: "waves",
    palette: ["#081c15", "#1b4332", "#40916c", "#95d5b2", "#d8f3dc"],
    ratio: 1.5,
  },
  {
    id: "letterpress",
    feed: "kiln-cog",
    title: "Restoring a letterpress that spent forty years in a barn",
    summary: "Rust, rollers and a surprisingly complete set of wood type.",
    hoursAgo: 46,
    size: "S",
    score: 0.28,
    scene: "poster",
    palette: ["#fefae0", "#bc6c25", "#283618", "#dda15e", "#606c38"],
    ratio: 1.33,
  },
  {
    id: "terminal",
    feed: "small-engines",
    title: "A terminal colour scheme you can actually read in sunlight",
    summary: "Contrast ratios, not vibes.",
    hoursAgo: 48,
    size: "S",
    score: 0.26,
  },
];

const feedTitle = (id: string) =>
  feeds.find((feed) => feed.id === id)?.title ?? id;

/** Deterministic noise, so every capture draws the same picture. */
function random(seed: string) {
  let state = 0;
  for (const char of seed) state = (state * 31 + char.charCodeAt(0)) >>> 0;
  return () => {
    state = (state * 1664525 + 1013904223) >>> 0;
    return state / 2 ** 32;
  };
}

function svg(item: ShowcaseItem, width: number, height: number) {
  const [a, b, c, d, e] = item.palette ?? [];
  const r = random(item.id);
  const W = width;
  const H = height;
  const ridge = (base: number, amp: number, fill: string) => {
    let path = `M0 ${H}L0 ${base}`;
    const steps = 9;
    for (let i = 1; i <= steps; i++) {
      const x = (W * i) / steps;
      const y = base - amp * (0.3 + r());
      path += `L${x.toFixed(0)} ${y.toFixed(0)}`;
    }
    return `<path fill="${fill}" d="${path}L${W} ${H}Z"/>`;
  };
  const sky = `<defs><linearGradient id="g" x2="0" y2="1"><stop stop-color="${a}"/><stop offset="1" stop-color="${e}"/></linearGradient></defs><rect width="${W}" height="${H}" fill="url(#g)"/>`;
  let body = "";
  switch (item.scene) {
    case "ridges":
      body =
        `<circle cx="${W * 0.72}" cy="${H * 0.3}" r="${H * 0.09}" fill="${e}" opacity=".9"/>` +
        ridge(H * 0.55, H * 0.25, b) +
        ridge(H * 0.7, H * 0.2, c) +
        ridge(H * 0.85, H * 0.15, d);
      break;
    case "horizon": {
      const sea = H * 0.58;
      const stars = Array.from(
        { length: 40 },
        () =>
          `<circle cx="${(r() * W).toFixed(0)}" cy="${(r() * sea * 0.7).toFixed(0)}" r="${(1 + r() * 2).toFixed(1)}" fill="${e}" opacity="${(0.3 + r() * 0.6).toFixed(2)}"/>`,
      ).join("");
      body =
        `<defs><linearGradient id="h" x2="0" y2="1"><stop stop-color="${a}"/><stop offset=".6" stop-color="${c}"/><stop offset="1" stop-color="${d}"/></linearGradient></defs><rect width="${W}" height="${sea}" fill="url(#h)"/>` +
        stars +
        `<circle cx="${W * 0.38}" cy="${sea - H * 0.06}" r="${H * 0.13}" fill="${d}"/>` +
        `<rect y="${sea}" width="${W}" height="${H - sea}" fill="${b}"/>` +
        Array.from(
          { length: 7 },
          (_, i) =>
            `<rect x="${(W * 0.38 - H * (0.12 - i * 0.012)).toFixed(0)}" y="${(sea + 14 + i * 22).toFixed(0)}" width="${(H * (0.24 - i * 0.024)).toFixed(0)}" height="6" rx="3" fill="${d}" opacity="${(0.7 - i * 0.08).toFixed(2)}"/>`,
        ).join("");
      break;
    }
    case "comet": {
      const stars = Array.from(
        { length: 120 },
        () =>
          `<circle cx="${(r() * W).toFixed(0)}" cy="${(r() * H).toFixed(0)}" r="${(1 + r() * 2.5).toFixed(1)}" fill="${e}" opacity="${(0.25 + r() * 0.7).toFixed(2)}"/>`,
      ).join("");
      const x = W * 0.68;
      const y = H * 0.5;
      const fragments = Array.from(
        { length: 6 },
        (_, i) =>
          `<circle cx="${(x - W * (0.06 + i * 0.05)).toFixed(0)}" cy="${(y - H * (0.012 + i * 0.012) + (r() - 0.5) * 18).toFixed(0)}" r="${(11 - i * 1.5).toFixed(1)}" fill="${e}" opacity="${(0.95 - i * 0.12).toFixed(2)}"/>`,
      ).join("");
      return (
        `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">` +
        `<defs><linearGradient id="s" x2="0" y2="1"><stop stop-color="${a}"/><stop offset="1" stop-color="${b}"/></linearGradient><linearGradient id="t" x1="1" x2="0"><stop stop-color="${d}" stop-opacity=".9"/><stop offset="1" stop-color="${c}" stop-opacity="0"/></linearGradient><radialGradient id="n"><stop stop-color="${e}"/><stop offset=".3" stop-color="${d}" stop-opacity=".85"/><stop offset="1" stop-color="${d}" stop-opacity="0"/></radialGradient></defs>` +
        `<rect width="${W}" height="${H}" fill="url(#s)"/>` +
        stars +
        `<path fill="url(#t)" d="M${x} ${y - 22}L${W * 0.05} ${H * 0.3}L${W * 0.05} ${H * 0.4}L${x} ${y + 22}Z"/>` +
        fragments +
        `<circle cx="${x}" cy="${y}" r="${H * 0.12}" fill="url(#n)"/>` +
        `<path fill="${a}" d="M0 ${H * 0.86}Q${W * 0.3} ${H * 0.8} ${W * 0.55} ${H * 0.85}T${W} ${H * 0.82}V${H}H0Z"/></svg>`
      );
    }
    case "poster": {
      body = `<rect width="${W}" height="${H}" fill="${a}"/>`;
      const cols = 4;
      const cell = W / cols;
      for (let row = 0; row * cell < H; row++)
        for (let col = 0; col < cols; col++) {
          const fill = [b, c, d, e][Math.floor(r() * 4)];
          const x = col * cell;
          const y = row * cell;
          const shape = Math.floor(r() * 3);
          body +=
            shape === 0
              ? `<circle cx="${x + cell / 2}" cy="${y + cell / 2}" r="${cell * 0.42}" fill="${fill}"/>`
              : shape === 1
                ? `<path d="M${x} ${y + cell}A${cell} ${cell} 0 0 1 ${x + cell} ${y}V${y + cell}Z" fill="${fill}"/>`
                : `<rect x="${x + cell * 0.08}" y="${y + cell * 0.08}" width="${cell * 0.84}" height="${cell * 0.84}" fill="${fill}"/>`;
        }
      return `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">${body}</svg>`;
    }
    case "skyline": {
      body = `<rect width="${W}" height="${H}" fill="${a}"/><circle cx="${W * 0.25}" cy="${H * 0.32}" r="${H * 0.16}" fill="${e}"/>`;
      let x = 0;
      while (x < W) {
        const w = W * (0.05 + r() * 0.08);
        const h = H * (0.25 + r() * 0.45);
        const fill = [b, c, d][Math.floor(r() * 3)];
        body += `<rect x="${x.toFixed(0)}" y="${(H - h).toFixed(0)}" width="${w.toFixed(0)}" height="${h.toFixed(0)}" fill="${fill}"/>`;
        x += w + W * 0.006;
      }
      break;
    }
    case "waves":
      body = "";
      for (let i = 0; i < 6; i++) {
        const y = H * (0.3 + i * 0.13);
        const fill = [b, c, d, b, c, d][i];
        body += `<path fill="${fill}" opacity=".92" d="M0 ${y}Q${W * 0.25} ${y - H * 0.08} ${W * 0.5} ${y}T${W} ${y}V${H}H0Z"/>`;
      }
      break;
    case "dunes":
      body =
        `<circle cx="${W * 0.66}" cy="${H * 0.36}" r="${H * 0.12}" fill="${e}"/>` +
        `<path fill="${d}" d="M0 ${H * 0.62}Q${W * 0.3} ${H * 0.45} ${W * 0.6} ${H * 0.6}T${W} ${H * 0.55}V${H}H0Z"/>` +
        `<path fill="${c}" d="M0 ${H * 0.75}Q${W * 0.4} ${H * 0.6} ${W * 0.75} ${H * 0.78}T${W} ${H * 0.72}V${H}H0Z"/>` +
        `<path fill="${b}" d="M0 ${H * 0.88}Q${W * 0.5} ${H * 0.76} ${W} ${H * 0.9}V${H}H0Z"/>`;
      break;
  }
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">${sky}${body}</svg>`;
}

function toItem(item: ShowcaseItem) {
  const now = Date.now();
  const published = new Date(now - item.hoursAgo * 3_600_000).toISOString();
  const width = 1600;
  const height = Math.round(width / (item.ratio ?? 1.5));
  return {
    item_id: item.id,
    story_id: item.story,
    feed_id: item.feed,
    feed_title: feedTitle(item.feed),
    connector: "rss",
    url: `https://example.com/${item.feed}/${item.id}`,
    title: item.title,
    summary: item.summary,
    summary_source: "feed",
    published_ts: published,
    fetched_ts: published,
    has_body: true,
    body_url: `/archive/${item.id}.html`,
    media_url: item.scene ? `/media/showcase/${item.id}.svg` : undefined,
    media_type: item.scene ? "image" : undefined,
    media_w: item.scene ? width : undefined,
    media_h: item.scene ? height : undefined,
    extract_quality: 0.9,
    score: item.score,
    size: item.size,
    read: item.read ?? false,
    signal: 0,
    hearted: false,
  };
}

/** Mocks the whole API with the showcase front page and opens the grid. */
export async function openShowcase(page: Page) {
  await page.addInitScript(() => localStorage.setItem("sema.signed-in", "1"));
  const all = items.map(toItem);
  const loose = all.filter((item) => !item.story_id);
  const storyIDs = [...new Set(all.flatMap((item) => item.story_id ?? []))];
  const stories = storyIDs.map((id, index) => {
    const members = all.filter((item) => item.story_id === id);
    return {
      story_id: id,
      source_count: members.length,
      order_key: index + 1,
      size: "L",
      items: members,
    };
  });
  const counts = Object.fromEntries(
    feeds.map((feed) => {
      const own = all.filter((item) => item.feed_id === feed.id);
      return [
        feed.id,
        { all: own.length, unread: own.filter((item) => !item.read).length },
      ];
    }),
  );

  await page.route("**/media/showcase/*.svg", (route) => {
    const id = new URL(route.request().url()).pathname
      .split("/")
      .pop()
      ?.replace(".svg", "");
    const item = items.find((candidate) => candidate.id === id);
    if (!item) return route.fulfill({ status: 404 });
    const width = 1600;
    const height = Math.round(width / (item.ratio ?? 1.5));
    return route.fulfill({
      contentType: "image/svg+xml",
      body: svg(item, width, height),
    });
  });
  await page.route("**/api/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    // The glob also matches Vite's own /src/api/ modules.
    if (!path.startsWith("/api/")) return route.continue();
    if (route.request().method() !== "GET")
      return route.fulfill({ status: 204 });
    if (path === "/api/me")
      return route.fulfill({
        json: {
          profile: {
            email: "reader@example.com",
            created_at: "2026-01-01T00:00:00Z",
            order_pref: "interest",
            heart_count: 42,
          },
          heart_count: 42,
          signal_count: 318,
          model: {
            explicit_count: 318,
            liked_count: 204,
            disliked_count: 114,
            implicit_count: 1290,
          },
        },
      });
    if (path === "/api/items")
      return route.fulfill({
        json: {
          items: url.searchParams.get("exclude_stories") ? loose : all,
          next_cursor: null,
        },
      });
    if (path === "/api/stories") return route.fulfill({ json: { stories } });
    if (path === "/api/feeds/counts")
      return route.fulfill({ json: { feeds: counts } });
    if (path === "/api/feeds")
      return route.fulfill({
        json: {
          feeds: feeds.map((feed) => ({
            feed_id: feed.id,
            url: `https://example.com/${feed.id}/feed.xml`,
            connector: "rss",
            title: feed.title,
            tags: feed.tags,
            muted: false,
            hide_shorts: false,
            always_generate: false,
            fetch_interval_h: 3,
            error_count: 0,
            next_fetch_at: new Date().toISOString(),
            prior: 0,
            prior_signals: 0,
            status: "ok",
            item_count: counts[feed.id].all,
            extraction_sample: 0,
          })),
        },
      });
    if (path === "/api/archive")
      return route.fulfill({ json: { items: [], next_cursor: null } });
    return route.fulfill({ json: {} });
  });
  await page.goto("/");
}
