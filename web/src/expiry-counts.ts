export function shouldRefreshExpiryCounts(
  expiring: boolean,
  now: number,
  lastRefresh: number,
): boolean {
  return expiring || now - lastRefresh >= 5 * 60_000;
}
