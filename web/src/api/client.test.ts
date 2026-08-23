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
