import { describe, expect, it } from "vitest";
import { PULL_THRESHOLD, RefreshGate, resistedPull } from "./pull-refresh";

describe("pull to refresh", () => {
  it("uses resistance and a bounded threshold", () => {
    expect(resistedPull(0)).toBe(0);
    expect(resistedPull(120)).toBeGreaterThanOrEqual(PULL_THRESHOLD);
    expect(resistedPull(1_000)).toBe(78);
  });

  it("is idempotent while a refresh is in flight", async () => {
    const gate = new RefreshGate();
    let calls = 0;
    let finish!: (value: number) => void;
    const task = () => {
      calls++;
      return new Promise<number>((resolve) => {
        finish = resolve;
      });
    };
    const first = gate.run(task);
    const second = gate.run(task);
    expect(calls).toBe(1);
    finish(3);
    await expect(first).resolves.toBe(3);
    await expect(second).resolves.toBe(3);
  });
});
