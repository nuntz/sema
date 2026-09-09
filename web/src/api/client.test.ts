import { afterEach, describe, expect, it, vi } from "vitest";
import { linkBehaviourEvent } from "../behaviour-events";
import {
  APIClient,
  APIError,
  clearSessionBootstrap,
  primeSessionBootstrap,
} from "./client";

afterEach(() => {
  clearSessionBootstrap();
  vi.unstubAllGlobals();
});

describe("behaviour event client", () => {
  it("posts copied-link events with shared true", async () => {
    const request = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", request);

    await new APIClient().events("item/with slash", linkBehaviourEvent());

    expect(request).toHaveBeenCalledOnce();
    const [path, init] = request.mock.calls[0];
    expect(init).toBeDefined();
    expect(path).toBe("/api/items/item%2Fwith%20slash/events");
    expect(init?.method).toBe("POST");
    expect(new Headers(init?.headers).has("Authorization")).toBe(false);
    expect(init?.body).toBe(
      JSON.stringify({ clicked_through: true, shared: true }),
    );
  });
});

describe("read state client", () => {
  it("persists both sides of an explicit read toggle", async () => {
    const request = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({ ok: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", request);

    const client = new APIClient();
    await client.read("item/with slash", true);
    await client.read("item/with slash", false);

    expect(request).toHaveBeenCalledTimes(2);
    expect(
      request.mock.calls.map(([path, init]) => [path, init?.body]),
    ).toEqual([
      ["/api/items/item%2Fwith%20slash/read", JSON.stringify({ read: true })],
      ["/api/items/item%2Fwith%20slash/read", JSON.stringify({ read: false })],
    ]);
  });
});

describe("story client", () => {
  it("keeps calendar bounds on story requests and continuation pages", async () => {
    const request = vi.fn(
      async (_input: RequestInfo | URL) =>
        new Response(
          JSON.stringify({ items: [], stories: [], next_cursor: null }),
        ),
    );
    vi.stubGlobal("fetch", request);
    const client = new APIClient();
    const window = {
      from: "2026-09-07T07:00:00.000Z",
      before: "2026-09-08T07:00:00.000Z",
    };
    await client.stories({ kind: "tag", value: "tech" }, true, window);
    await client.items(
      "interest",
      "next-page",
      true,
      { kind: "tag", value: "tech" },
      false,
      window,
    );
    for (const [path] of request.mock.calls) {
      const params = new URL(String(path), "https://example.com").searchParams;
      expect(params.get("fetched_from")).toBe(window.from);
      expect(params.get("fetched_before")).toBe(window.before);
      expect(params.get("include_read")).toBe("true");
      expect(params.get("tag")).toBe("tech");
    }
    expect(
      new URL(
        String(request.mock.calls[1][0]),
        "https://example.com",
      ).searchParams.get("cursor"),
    ).toBe("next-page");
  });

  it("maps tag and feed scopes and excludes rendered stories from item pages", async () => {
    const request = vi.fn(async (input: RequestInfo | URL) =>
      input.toString().includes("/stories")
        ? new Response(JSON.stringify({ stories: [] }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          })
        : new Response(JSON.stringify({ items: [], next_cursor: null }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
    );
    vi.stubGlobal("fetch", request);

    const client = new APIClient();
    await client.stories({ kind: "tag", value: "untagged" }, true);
    await client.items(
      "interest",
      "cursor",
      true,
      { kind: "tag", value: "untagged" },
      true,
    );
    await client.stories({ kind: "feed", value: "feed/id" });
    await client.items("chrono", "", false, {
      kind: "feed",
      value: "feed/id",
    });

    expect(request.mock.calls.map(([path]) => path)).toEqual([
      "/api/stories?include_read=true&tag=__untagged",
      "/api/items?order=interest&limit=100&cursor=cursor&include_read=true&tag=__untagged&exclude_stories=true",
      "/api/stories?feed=feed%2Fid",
      "/api/items?order=chrono&limit=100&feed=feed%2Fid",
    ]);
  });
});

describe("feed management client", () => {
  it("loads retained and unread counts for grid scopes", async () => {
    const request = vi.fn(
      async () =>
        new Response(
          JSON.stringify({ feeds: { daily: { all: 12, unread: 3 } } }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        ),
    );
    vi.stubGlobal("fetch", request);

    await expect(new APIClient().feedItemCounts()).resolves.toEqual({
      daily: { all: 12, unread: 3 },
    });
    expect(request).toHaveBeenCalledWith("/api/feeds/counts", {
      headers: expect.any(Headers),
    });
  });

  it("wires feed detail edits to the encoded PATCH route", async () => {
    const request = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({ feed_id: "feed/id" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", request);

    await new APIClient().patchFeed("feed/id", {
      custom_title: "Custom",
      tags: ["dev"],
      muted: true,
      fetch_interval_h: 6,
    });

    const [path, init] = request.mock.calls[0];
    expect(path).toBe("/api/feeds/feed%2Fid");
    expect(init?.method).toBe("PATCH");
    expect(new Headers(init?.headers).has("Authorization")).toBe(false);
    expect(init?.body).toBe(
      JSON.stringify({
        custom_title: "Custom",
        tags: ["dev"],
        muted: true,
        fetch_interval_h: 6,
      }),
    );
  });

  it("preserves structured Reddit discovery errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
              error: "Reddit is rate-limiting us.",
              kind: "rate_limited",
              upstream_status: 429,
            }),
            {
              status: 422,
              headers: { "Content-Type": "application/json" },
            },
          ),
      ),
    );

    const failure = await new APIClient()
      .discoverFeed("r/castles")
      .catch((error: unknown) => error);

    expect(failure).toBeInstanceOf(APIError);
    expect(failure).toMatchObject({
      message: "Reddit is rate-limiting us.",
      status: 422,
      kind: "rate_limited",
      upstreamStatus: 429,
    });
  });
});

describe("session authentication", () => {
  it("sends no Authorization header on ordinary or export requests", async () => {
    const request = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ profile: {} }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      )
      .mockResolvedValueOnce(new Response("<opml />", { status: 200 }));
    vi.stubGlobal("fetch", request);

    const client = new APIClient();
    await client.me();
    await client.exportOPML();

    const [, meInit] = request.mock.calls[0];
    expect(new Headers(meInit?.headers).has("Authorization")).toBe(false);
    expect(request.mock.calls[1]).toEqual(["/api/feeds/export.opml"]);
  });

  it("boots from the session exchange payload without repeating /me", async () => {
    const request = vi.fn();
    vi.stubGlobal("fetch", request);
    const me = {
      profile: {
        email: "reader@example.com",
        created_at: "",
        order_pref: "interest" as const,
        heart_count: 0,
      },
      signal_count: 0,
      heart_count: 0,
      model: {
        explicit_count: 0,
        liked_count: 0,
        disliked_count: 0,
        implicit_count: 0,
      },
    };
    primeSessionBootstrap(me);

    await expect(new APIClient().me()).resolves.toBe(me);
    expect(request).not.toHaveBeenCalled();
  });
});

describe("archive filters", () => {
  it("sends tag and feed scopes on initial and subsequent pages", async () => {
    const request = vi.fn(
      async (_input: RequestInfo | URL) =>
        new Response(JSON.stringify({ items: [] })),
    );
    vi.stubGlobal("fetch", request);
    const client = new APIClient();
    await client.archive("", { kind: "tag", value: "tech" });
    await client.archive("next-page", { kind: "tag", value: "tech" });
    await client.archive("", { kind: "tag", value: "untagged" });
    await client.archive("", { kind: "feed", value: "daily" });
    await client.archive();
    expect(request.mock.calls.map((call) => call[0])).toEqual([
      "/api/archive?limit=100&tag=tech",
      "/api/archive?limit=100&cursor=next-page&tag=tech",
      "/api/archive?limit=100&tag=__untagged",
      "/api/archive?limit=100&feed=daily",
      "/api/archive?limit=100",
    ]);
  });
});
