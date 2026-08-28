import type { Item } from "../types";

export interface LayoutCell {
  item: Item;
  width: number;
  left: number;
  effectiveSize: "S" | "M" | "L";
  height?: number;
  offsetY?: number;
  span?: 2;
  tall?: true;
}

export interface LayoutRow {
  cells: LayoutCell[];
  height: number;
  top: number;
  gap: number;
  kind: "span" | "tall" | "hero" | "pair" | "standard" | "compact";
}

const desktopGap = 10;
const mobileGap = 8;
const standardHeight = 172;
const spanHeight = 354;
const tallHeight = 288;
const compactHeight = 132;
const spanAspectMax = 1.2;
const spanCompanionCount = 5;
const tallCompanionCount = 3;
const mosaicMinimumCompanions = 3;
const sizeFactor = { S: 0.8, M: 1.05, L: 1.6 } as const;
const tallLargeFactor = 1.75;

function aspect(item: Item): number {
  if (!item.media_w || !item.media_h) return 1.4;
  return Math.max(0.75, Math.min(2.1, item.media_w / item.media_h));
}

export function spanEligible(item: Item): boolean {
  return (
    item.size === "L" &&
    Boolean(item.media_url) &&
    Boolean(item.media_w && item.media_h) &&
    (item.media_w ?? 0) / (item.media_h ?? 1) <= spanAspectMax
  );
}

const largeRunAnchors = [
  [0.75, 0.9],
  [1, 1],
  [1.5, 1.2],
  [16 / 9, 1.5],
  [2, 1.6],
] as const;

export function largeRunWeight(item: Item): number {
  if (!item.media_url || !item.media_w || !item.media_h) return 1;
  const ratio = clamp(item.media_w / item.media_h, 0.75, 2);
  for (let index = 1; index < largeRunAnchors.length; index++) {
    const [rightAspect, rightWeight] = largeRunAnchors[index];
    if (ratio > rightAspect) continue;
    const [leftAspect, leftWeight] = largeRunAnchors[index - 1];
    const progress = (ratio - leftAspect) / (rightAspect - leftAspect);
    return leftWeight + (rightWeight - leftWeight) * progress;
  }
  return 1.6;
}

function naturalWidth(
  item: Item,
  baseUnit: number,
  largeFactor: number = sizeFactor.L,
): number {
  const factor = item.size === "L" ? largeFactor : sizeFactor[item.size];
  return baseUnit * factor * Math.sqrt(aspect(item));
}

function topAfter(rows: LayoutRow[], gap: number): number {
  const last = rows.at(-1);
  return last ? last.top + last.height + gap : 0;
}

function clamp(value: number, low: number, high: number): number {
  return Math.max(low, Math.min(high, value));
}

function cellsWithWidths(
  items: Item[],
  widths: number[],
  startLeft: number,
  offsetY: number,
  height: number,
  gap: number,
): LayoutCell[] {
  let left = startLeft;
  return items.map((item, index) => {
    const width = widths[index] ?? 0;
    const cell: LayoutCell = {
      item,
      width,
      left,
      effectiveSize: item.size,
      height,
      offsetY,
    };
    left += width + gap;
    return cell;
  });
}

function weightedLineCells(
  items: Item[],
  weights: number[],
  availableWidth: number,
  height: number,
  gap: number,
): LayoutCell[] {
  const contentWidth = availableWidth - gap * Math.max(0, items.length - 1);
  const total = weights.reduce((sum, weight) => sum + weight, 0);
  const widths = weights.map((weight) => (contentWidth * weight) / total);
  const usedBeforeLast = widths
    .slice(0, -1)
    .reduce((sum, width) => sum + width, 0);
  widths[widths.length - 1] = contentWidth - usedBeforeLast;
  return cellsWithWidths(items, widths, 0, 0, height, gap);
}

function justifiedLineCells(
  items: Item[],
  availableWidth: number,
  startLeft: number,
  offsetY: number,
  height: number,
  gap: number,
  baseUnit: number,
  largeFactor: number = sizeFactor.L,
): LayoutCell[] {
  if (items.length === 0) return [];
  const natural = items.map((item) =>
    naturalWidth(item, baseUnit, largeFactor),
  );
  const contentWidth = availableWidth - gap * Math.max(0, items.length - 1);
  const scale = contentWidth / natural.reduce((sum, width) => sum + width, 0);
  const widths = natural.map((width) => width * scale);
  const usedBeforeLast = widths
    .slice(0, -1)
    .reduce((sum, width) => sum + width, 0);
  widths[widths.length - 1] = contentWidth - usedBeforeLast;
  return cellsWithWidths(items, widths, startLeft, offsetY, height, gap);
}

function naturalLineCells(
  items: Item[],
  availableWidth: number,
  startLeft: number,
  offsetY: number,
  height: number,
  gap: number,
  baseUnit: number,
  largeFactor: number = sizeFactor.L,
): LayoutCell[] {
  if (items.length === 0) return [];
  const natural = items.map((item) =>
    naturalWidth(item, baseUnit, largeFactor),
  );
  const contentWidth = availableWidth - gap * Math.max(0, items.length - 1);
  const naturalTotal = natural.reduce((sum, width) => sum + width, 0);
  const scale = Math.min(1, contentWidth / naturalTotal);
  return cellsWithWidths(
    items,
    natural.map((width) => width * scale),
    startLeft,
    offsetY,
    height,
    gap,
  );
}

function followingCompanions(
  items: Item[],
  start: number,
  limit: number,
): Item[] {
  const companions: Item[] = [];
  for (
    let index = start;
    index < items.length && companions.length < limit;
    index++
  ) {
    if (items[index].size === "L") break;
    companions.push(items[index]);
  }
  return companions;
}

function companionCountBeforeLarge(items: Item[], start: number): number {
  let count = 0;
  for (let index = start; index < items.length; index++) {
    if (items[index].size === "L") break;
    count++;
  }
  return count;
}

function stablePrefixLength(items: Item[], hasMore: boolean): number {
  if (!hasMore) return items.length;
  for (let index = 0; index < items.length; index++) {
    if (items[index].size !== "L") continue;
    let resolved = false;
    for (
      let lookahead = index + 1;
      lookahead < items.length && lookahead <= index + 3;
      lookahead++
    ) {
      if (items[lookahead].size === "L" || lookahead === index + 3) {
        resolved = true;
        break;
      }
    }
    if (!resolved) {
      let runStart = index;
      while (runStart > 0 && items[runStart - 1].size === "L") runStart--;
      return runStart;
    }
  }
  return items.length;
}

function desktopSpanBand(
  hero: Item,
  companions: Item[],
  containerWidth: number,
  heroRight: boolean,
  top: number,
  naturalCompanions: boolean,
): LayoutRow {
  const subHeight = (spanHeight - desktopGap) / 2;
  const heroWidth = ((containerWidth - desktopGap) * 1.1) / (1.1 + 2.5);
  const columnWidth = containerWidth - desktopGap - heroWidth;
  const heroLeft = heroRight ? columnWidth + desktopGap : 0;
  const columnLeft = heroRight ? 0 : heroWidth + desktopGap;
  const split = Math.min(3, Math.ceil(companions.length / 2));
  const baseUnit = clamp(containerWidth / 8, 92, 150);
  const line = naturalCompanions ? naturalLineCells : justifiedLineCells;
  const cells: LayoutCell[] = [
    {
      item: hero,
      width: heroWidth,
      left: heroLeft,
      effectiveSize: "L",
      height: spanHeight,
      offsetY: 0,
      span: 2,
    },
    ...line(
      companions.slice(0, split),
      columnWidth,
      columnLeft,
      0,
      subHeight,
      desktopGap,
      baseUnit,
    ),
    ...line(
      companions.slice(split),
      columnWidth,
      columnLeft,
      subHeight + desktopGap,
      subHeight,
      desktopGap,
      baseUnit,
    ),
  ];
  return {
    cells,
    height: spanHeight,
    top,
    gap: desktopGap,
    kind: "span",
  };
}

function desktopTallBand(
  items: Item[],
  heroIndex: number,
  containerWidth: number,
  top: number,
  natural: boolean,
): LayoutRow {
  const baseUnit = clamp(containerWidth / 8, 92, 150);
  const line = natural ? naturalLineCells : justifiedLineCells;
  const cells = line(
    items,
    containerWidth,
    0,
    0,
    tallHeight,
    desktopGap,
    baseUnit,
    tallLargeFactor,
  );
  const hero = cells[heroIndex];
  if (hero) hero.tall = true;
  return {
    cells,
    height: tallHeight,
    top,
    gap: desktopGap,
    kind: "tall",
  };
}

function desktopLargeBand(
  items: Item[],
  containerWidth: number,
  top: number,
  pair: boolean,
): LayoutRow {
  const height = pair ? spanHeight : tallHeight;
  return {
    cells: weightedLineCells(
      items,
      pair ? items.map(() => 1) : items.map(largeRunWeight),
      containerWidth,
      height,
      desktopGap,
    ),
    height,
    top,
    gap: desktopGap,
    kind: pair ? "pair" : "hero",
  };
}

function appendDesktopLargeRun(
  rows: LayoutRow[],
  items: Item[],
  containerWidth: number,
): void {
  let index = 0;
  let previousPair = false;
  while (index < items.length) {
    const remaining = items.length - index;
    const pair =
      remaining >= 2 &&
      remaining !== 3 &&
      !previousPair &&
      spanEligible(items[index]) &&
      spanEligible(items[index + 1]);
    if (pair) {
      rows.push(
        desktopLargeBand(
          items.slice(index, index + 2),
          containerWidth,
          topAfter(rows, desktopGap),
          true,
        ),
      );
      previousPair = true;
      index += 2;
      continue;
    }

    const count = remaining === 4 ? 2 : Math.min(3, remaining);
    rows.push(
      desktopLargeBand(
        items.slice(index, index + count),
        containerWidth,
        topAfter(rows, desktopGap),
        false,
      ),
    );
    previousPair = false;
    index += count;
  }
}

function regularGroups(items: Item[], followedByLarge: boolean) {
  const allSmall = items.every((item) => item.size === "S");
  const groupSize = allSmall ? 6 : 5;
  const groups: Item[][] = [];
  for (let index = 0; index < items.length; index += groupSize)
    groups.push(items.slice(index, index + groupSize));

  let stragglers: Item[] = [];
  const tail = groups.at(-1);
  if (followedByLarge && tail && tail.length <= 2) {
    stragglers = tail;
    groups.pop();
  }
  return { groups, stragglers };
}

function appendDesktopRegularRows(
  rows: LayoutRow[],
  groups: Item[][],
  containerWidth: number,
  finalRun: boolean,
): void {
  const baseUnit = clamp(containerWidth / 8, 92, 150);
  groups.forEach((group, index) => {
    const final = finalRun && index === groups.length - 1;
    const compact =
      group.length >= 5 && group.every((item) => item.size === "S");
    let cells: LayoutCell[];
    let height: number;
    if (compact) {
      const cellWidth = Math.min(
        compactHeight,
        (containerWidth - desktopGap * Math.max(0, group.length - 1)) /
          group.length,
      );
      cells = cellsWithWidths(
        group,
        group.map(() => cellWidth),
        0,
        0,
        compactHeight,
        desktopGap,
      );
      height = compactHeight;
    } else {
      const line = final ? naturalLineCells : justifiedLineCells;
      const rowHeight = final
        ? (rows.at(-1)?.height ?? standardHeight)
        : standardHeight;
      cells = line(
        group,
        containerWidth,
        0,
        0,
        rowHeight,
        desktopGap,
        baseUnit,
      );
      height = rowHeight;
    }
    rows.push({
      cells,
      height,
      top: topAfter(rows, desktopGap),
      gap: desktopGap,
      kind: compact ? "compact" : "standard",
    });
  });
}

function appendAbsorbedTallBand(
  rows: LayoutRow[],
  items: Item[],
  largeIndex: number,
  leading: Item[],
  containerWidth: number,
  finalFeed: boolean,
): number {
  const large = items[largeIndex];
  const following = followingCompanions(
    items,
    largeIndex + 1,
    tallCompanionCount - leading.length,
  );
  const nextIndex = largeIndex + 1 + following.length;
  rows.push(
    desktopTallBand(
      [...leading, large, ...following],
      leading.length,
      containerWidth,
      topAfter(rows, desktopGap),
      finalFeed && nextIndex === items.length,
    ),
  );
  return nextIndex;
}

export function justify(
  items: Item[],
  containerWidth: number,
  hasMore = false,
  options: { completeSegment?: boolean } = {},
): LayoutRow[] {
  if (containerWidth <= 0 || items.length === 0) return [];
  const stableLength = options.completeSegment
    ? items.length
    : stablePrefixLength(items, hasMore);
  const layoutItems = items.slice(0, stableLength);
  const finalFeed = !hasMore && stableLength === items.length;
  if (containerWidth < 700)
    return mobileRows(layoutItems, containerWidth, finalFeed);

  const rows: LayoutRow[] = [];
  let spanIndex = 0;
  for (let index = 0; index < layoutItems.length; ) {
    const item = layoutItems[index];
    if (item.size === "L") {
      let runEnd = index + 1;
      while (runEnd < layoutItems.length && layoutItems[runEnd].size === "L")
        runEnd++;
      const runLength = runEnd - index;
      const boundaryCompanions = companionCountBeforeLarge(layoutItems, runEnd);
      const mosaicTransition =
        runLength >= 3 && boundaryCompanions >= mosaicMinimumCompanions;
      if (runLength >= 2) {
        const largeRunEnd = mosaicTransition ? runEnd - 1 : runEnd;
        appendDesktopLargeRun(
          rows,
          layoutItems.slice(index, largeRunEnd),
          containerWidth,
        );
        index = largeRunEnd;
        continue;
      }

      const companions = followingCompanions(
        layoutItems,
        index + 1,
        spanCompanionCount,
      );
      const opensMosaic = boundaryCompanions >= mosaicMinimumCompanions;
      const finalBand =
        finalFeed && index + 1 + boundaryCompanions === layoutItems.length;
      const adjacentSpan = rows.at(-1)?.kind === "span";
      const opensSpan =
        spanEligible(item) &&
        !adjacentSpan &&
        ((opensMosaic && companions.length >= spanCompanionCount) ||
          (finalBand && companions.length > 0));
      if (opensSpan) {
        const nextIndex = index + 1 + companions.length;
        const short = companions.length < spanCompanionCount;
        rows.push(
          desktopSpanBand(
            item,
            companions,
            containerWidth,
            spanIndex % 2 === 1,
            topAfter(rows, desktopGap),
            short || (finalFeed && nextIndex === layoutItems.length),
          ),
        );
        spanIndex++;
        index = nextIndex;
        continue;
      }

      const tallCompanions = followingCompanions(
        layoutItems,
        index + 1,
        tallCompanionCount,
      );
      const nextIndex = index + 1 + tallCompanions.length;
      rows.push(
        desktopTallBand(
          [item, ...tallCompanions],
          0,
          containerWidth,
          topAfter(rows, desktopGap),
          tallCompanions.length === 0 ||
            (finalFeed && nextIndex === layoutItems.length),
        ),
      );
      index = nextIndex;
      continue;
    }

    let end = index;
    while (end < layoutItems.length && layoutItems[end].size !== "L") end++;
    const followedByLarge = end < layoutItems.length;
    const { groups, stragglers } = regularGroups(
      layoutItems.slice(index, end),
      followedByLarge,
    );
    appendDesktopRegularRows(
      rows,
      groups,
      containerWidth,
      finalFeed && !followedByLarge,
    );
    if (stragglers.length > 0 && followedByLarge) {
      index = appendAbsorbedTallBand(
        rows,
        layoutItems,
        end,
        stragglers,
        containerWidth,
        finalFeed,
      );
      continue;
    }
    index = end;
  }
  return rows;
}

function appendMobileLargeBand(
  rows: LayoutRow[],
  items: Item[],
  containerWidth: number,
  kind: "tall" | "hero" | "pair",
): void {
  const height = kind === "hero" ? 208 : 246;
  const cells =
    items.length === 1
      ? cellsWithWidths(items, [containerWidth], 0, 0, height, mobileGap)
      : weightedLineCells(
          items,
          items.map(() => 1),
          containerWidth,
          height,
          mobileGap,
        );
  if (kind === "tall" && cells[0]) cells[0].tall = true;
  rows.push({
    cells,
    height,
    top: topAfter(rows, mobileGap),
    gap: mobileGap,
    kind,
  });
}

function appendMobileLargeRun(
  rows: LayoutRow[],
  items: Item[],
  containerWidth: number,
): void {
  let index = 0;
  const firstPairPortrait =
    items.length >= 2 && spanEligible(items[0]) && spanEligible(items[1]);
  if (items.length % 2 === 1 || !firstPairPortrait) {
    appendMobileLargeBand(rows, items.slice(0, 1), containerWidth, "tall");
    index = 1;
  }
  while (index + 1 < items.length) {
    const pair = items.slice(index, index + 2);
    appendMobileLargeBand(
      rows,
      pair,
      containerWidth,
      pair.every(spanEligible) ? "pair" : "hero",
    );
    index += 2;
  }
  if (index < items.length)
    appendMobileLargeBand(rows, items.slice(index), containerWidth, "tall");
}

function mobileRows(
  items: Item[],
  containerWidth: number,
  finalFeed: boolean,
): LayoutRow[] {
  const rows: LayoutRow[] = [];
  const baseUnit = 120;
  for (let index = 0; index < items.length; ) {
    const item = items[index];
    if (item.size === "L") {
      let runEnd = index + 1;
      while (runEnd < items.length && items[runEnd].size === "L") runEnd++;
      appendMobileLargeRun(rows, items.slice(index, runEnd), containerWidth);
      index = runEnd;
      continue;
    }

    const triple = items.slice(index, index + 3);
    if (triple.length === 3 && triple.every((entry) => entry.size === "S")) {
      const height = 112;
      const final = finalFeed && index + triple.length === items.length;
      const cells = final
        ? naturalLineCells(
            triple,
            containerWidth,
            0,
            0,
            height,
            mobileGap,
            baseUnit,
          )
        : justifiedLineCells(
            triple,
            containerWidth,
            0,
            0,
            height,
            mobileGap,
            baseUnit,
          );
      rows.push({
        cells,
        height,
        top: topAfter(rows, mobileGap),
        gap: mobileGap,
        kind: "compact",
      });
      index += 3;
      continue;
    }

    const next = items[index + 1];
    const height = 152;
    if (next && next.size !== "L") {
      const pair = [item, next];
      const final = finalFeed && index + 2 === items.length;
      const line = final ? naturalLineCells : justifiedLineCells;
      rows.push({
        cells: line(pair, containerWidth, 0, 0, height, mobileGap, baseUnit),
        height,
        top: topAfter(rows, mobileGap),
        gap: mobileGap,
        kind: "standard",
      });
      index += 2;
      continue;
    }

    const singleHeight = item.size === "S" ? 112 : 152;
    const final = finalFeed && index + 1 === items.length;
    const line = final ? naturalLineCells : justifiedLineCells;
    rows.push({
      cells: line(
        [item],
        containerWidth,
        0,
        0,
        singleHeight,
        mobileGap,
        baseUnit,
      ),
      height: singleHeight,
      top: topAfter(rows, mobileGap),
      gap: mobileGap,
      kind: "standard",
    });
    index++;
  }
  return rows;
}

export function totalHeight(rows: LayoutRow[]): number {
  const last = rows.at(-1);
  return last ? last.top + last.height : 0;
}

export function visibleRows(
  rows: LayoutRow[],
  scrollTop: number,
  viewportHeight: number,
  overscan = 360,
): LayoutRow[] {
  const start = Math.max(0, scrollTop - overscan);
  const end = scrollTop + viewportHeight + overscan;
  return rows.filter((row) => row.top + row.height >= start && row.top <= end);
}
