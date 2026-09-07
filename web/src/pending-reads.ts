// Failed batches remain pending. Explicit read changes invalidate an in-flight
// batch's retry so an old failure cannot undo a newer user action.
export class PendingReads {
  private readonly pending = new Set<string>();
  private readonly inFlight = new Map<string, symbol>();

  get size(): number {
    return this.pending.size;
  }

  [Symbol.iterator](): SetIterator<string> {
    return this.pending[Symbol.iterator]();
  }

  add(id: string): void {
    this.inFlight.delete(id);
    this.pending.add(id);
  }

  delete(id: string): boolean {
    this.inFlight.delete(id);
    return this.pending.delete(id);
  }

  async flush(write: (ids: string[]) => Promise<void>): Promise<void> {
    const ids = [...this.pending];
    this.pending.clear();
    const token = Symbol();
    for (const id of ids) this.inFlight.set(id, token);
    const results = await Promise.allSettled(
      Array.from({ length: Math.ceil(ids.length / 100) }, async (_, index) => {
        const batch = ids.slice(index * 100, (index + 1) * 100);
        try {
          await write(batch);
        } catch (error) {
          for (const id of batch) {
            if (this.inFlight.get(id) === token) this.pending.add(id);
          }
          throw error;
        } finally {
          for (const id of batch) {
            if (this.inFlight.get(id) === token) this.inFlight.delete(id);
          }
        }
      }),
    );
    for (const result of results) {
      if (result.status === "rejected") throw result.reason;
    }
  }
}
