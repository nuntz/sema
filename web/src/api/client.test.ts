import { afterEach, describe, expect, it, vi } from "vitest";
import { linkBehaviourEvent } from "../behaviour-events";
import { APIClient } from "./client";

afterEach(() => vi.unstubAllGlobals());

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

    await new APIClient(() => "token").events(
      "item/with slash",
      linkBehaviourEvent(),
    );

    expect(request).toHaveBeenCalledOnce();
    const [path, init] = request.mock.calls[0];
    expect(init).toBeDefined();
    expect(path).toBe("/api/items/item%2Fwith%20slash/events");
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(
      JSON.stringify({ clicked_through: true, shared: true }),
    );
  });
});

describe("feed management client", () => {
  it("wires feed detail edits to the encoded PATCH route", async () => {
    const request = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({ feed_id: "feed/id" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    vi.stubGlobal("fetch", request);

    await new APIClient(() => "token").patchFeed("feed/id", {
      custom_title: "Custom",
      tags: ["dev"],
      muted: true,
      fetch_interval_h: 6,
    });

    const [path, init] = request.mock.calls[0];
    expect(path).toBe("/api/feeds/feed%2Fid");
    expect(init?.method).toBe("PATCH");
    expect(init?.body).toBe(
      JSON.stringify({
        custom_title: "Custom",
        tags: ["dev"],
        muted: true,
        fetch_interval_h: 6,
      }),
    );
  });
});
