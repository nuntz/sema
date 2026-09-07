import { describe, expect, it, vi } from "vitest";
import { PendingReads } from "./pending-reads";

describe("pending reads", () => {
  it("retains a failed batch and sends it again on the next flush", async () => {
    const pending = new PendingReads();
    pending.add("article");
    const write = vi.fn().mockRejectedValueOnce(new Error("offline"));
    await expect(pending.flush(write)).rejects.toThrow("offline");
    expect([...pending]).toEqual(["article"]);
    write.mockResolvedValue(undefined);
    await pending.flush(write);
    expect(write).toHaveBeenNthCalledWith(2, ["article"]);
    expect(pending.size).toBe(0);
  });

  it("retries only failed batches when some requests succeed", async () => {
    const pending = new PendingReads();
    const ids = Array.from({ length: 201 }, (_, index) => String(index));
    for (const id of ids) pending.add(id);
    const write = vi.fn(async (batch: string[]) => {
      if (batch[0] === "100") throw new Error("unavailable");
    });
    await expect(pending.flush(write)).rejects.toThrow("unavailable");
    expect(write.mock.calls.map(([batch]) => batch.length)).toEqual([
      100, 100, 1,
    ]);
    expect([...pending]).toEqual(ids.slice(100, 200));
  });

  it("does not restore a failed read after Undo or an explicit read change", async () => {
    const pending = new PendingReads();
    pending.add("article");
    let fail!: (error: Error) => void;
    const flushing = pending.flush(
      () =>
        new Promise<void>((_resolve, reject) => {
          fail = reject;
        }),
    );
    expect(pending.delete("article")).toBe(false);
    fail(new Error("offline"));
    await expect(flushing).rejects.toThrow("offline");
    expect(pending.size).toBe(0);
  });

  it("does not resurrect an old failure after a newer read succeeds", async () => {
    const pending = new PendingReads();
    pending.add("article");
    let fail!: (error: Error) => void;
    const first = pending.flush(
      () =>
        new Promise<void>((_resolve, reject) => {
          fail = reject;
        }),
    );
    pending.add("article");
    await pending.flush(async () => {});
    fail(new Error("old request failed"));
    await expect(first).rejects.toThrow("old request failed");
    expect(pending.size).toBe(0);
  });

  it("preserves reads queued while a batch is in flight", async () => {
    const pending = new PendingReads();
    pending.add("first");
    let complete!: () => void;
    const flushing = pending.flush(
      () =>
        new Promise<void>((resolve) => {
          complete = resolve;
        }),
    );
    pending.add("second");
    complete();
    await flushing;
    expect([...pending]).toEqual(["second"]);
  });
});
