import { afterEach, describe, expect, it, vi } from "vitest";
import { clearSessionBootstrap } from "../api/client";
import { deleteSession, signedInKey, signInWithCredential } from "./google";

afterEach(() => {
  clearSessionBootstrap();
  vi.unstubAllGlobals();
});

describe("Google session exchange", () => {
  it("stores only the signed-in flag after the backend accepts the credential", async () => {
    const request = vi.fn(async () =>
      Response.json({
        profile: { email: "reader@example.com", order_pref: "interest" },
        signal_count: 0,
        heart_count: 0,
        model: {},
      }),
    );
    const storage = { setItem: vi.fn() };
    vi.stubGlobal("fetch", request);

    await signInWithCredential("google-id-token", storage);

    expect(request).toHaveBeenCalledWith("/api/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ credential: "google-id-token" }),
    });
    expect(storage.setItem).toHaveBeenCalledWith(signedInKey, "1");
    expect(storage.setItem).not.toHaveBeenCalledWith(
      expect.anything(),
      "google-id-token",
    );
  });

  it("does not mark the user signed in when the exchange is rejected", async () => {
    const storage = { setItem: vi.fn() };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 401 })),
    );

    await expect(
      signInWithCredential("rejected-token", storage),
    ).rejects.toThrow("not accepted");
    expect(storage.setItem).not.toHaveBeenCalled();
  });

  it("deletes the backend session and clears the local flag on sign-out", async () => {
    const request = vi.fn(async () => new Response(null, { status: 204 }));
    const storage = { removeItem: vi.fn() };
    vi.stubGlobal("fetch", request);

    await deleteSession(storage);

    expect(storage.removeItem).toHaveBeenCalledWith(signedInKey);
    expect(request).toHaveBeenCalledWith("/api/session", {
      method: "DELETE",
    });
  });
});
