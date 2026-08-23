export const PULL_THRESHOLD = 64;

export function resistedPull(distance: number): number {
  return Math.max(0, Math.min(78, distance * 0.55));
}

export class RefreshGate {
  private running?: Promise<number>;

  run(task: () => Promise<number>): Promise<number> {
    if (this.running) return this.running;
    this.running = task().finally(() => {
      this.running = undefined;
    });
    return this.running;
  }
}
