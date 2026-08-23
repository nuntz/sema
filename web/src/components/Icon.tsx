import Archive from "lucide-solid/icons/archive";
import ArrowUpDown from "lucide-solid/icons/arrow-up-down";
import Check from "lucide-solid/icons/check";
import ChevronDown from "lucide-solid/icons/chevron-down";
import ChevronLeft from "lucide-solid/icons/chevron-left";
import ChevronRight from "lucide-solid/icons/chevron-right";
import Clock from "lucide-solid/icons/clock";
import Download from "lucide-solid/icons/download";
import ExternalLink from "lucide-solid/icons/external-link";
import Heart from "lucide-solid/icons/heart";
import Link from "lucide-solid/icons/link";
import Pause from "lucide-solid/icons/pause";
import Plus from "lucide-solid/icons/plus";
import RotateCw from "lucide-solid/icons/rotate-cw";
import Search from "lucide-solid/icons/search";
import ThumbsDown from "lucide-solid/icons/thumbs-down";
import ThumbsUp from "lucide-solid/icons/thumbs-up";
import Trash2 from "lucide-solid/icons/trash-2";
import Unplug from "lucide-solid/icons/unplug";
import Upload from "lucide-solid/icons/upload";
import X from "lucide-solid/icons/x";
import { type JSX, splitProps } from "solid-js";
import { Dynamic } from "solid-js/web";

export type IconSize = 14 | 16 | 18 | 20 | 24;

type SemaRowsProps = JSX.SvgSVGAttributes<SVGSVGElement> & {
  size?: string | number;
  strokeWidth?: string | number;
};

function SemaRows(props: SemaRowsProps) {
  const [local, rest] = splitProps(props, ["size", "strokeWidth", "fill"]);
  return (
    <svg
      {...rest}
      xmlns="http://www.w3.org/2000/svg"
      width={local.size ?? 24}
      height={local.size ?? 24}
      viewBox="0 0 24 24"
      fill={local.fill ?? "none"}
      stroke="currentColor"
      stroke-width={local.strokeWidth ?? 2}
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <rect x="3" y="4" width="9" height="7" rx="1" />
      <rect x="14" y="4" width="7" height="7" rx="1" />
      <rect x="3" y="13" width="7" height="7" rx="1" />
      <rect x="12" y="13" width="9" height="7" rx="1" />
    </svg>
  );
}

const glyphs = {
  "add-feed": Plus,
  archive: Archive,
  "back-to-grid": SemaRows,
  check: Check,
  close: X,
  "copy-link": Link,
  "export-opml": Download,
  "feed-fallback": SemaRows,
  "import-opml": Upload,
  keep: Heart,
  menu: ChevronDown,
  mute: Pause,
  "next-item": ChevronRight,
  "open-original": ExternalLink,
  "previous-item": ChevronLeft,
  "remove-feed": Trash2,
  retry: RotateCw,
  search: Search,
  sort: ArrowUpDown,
  "status-broken": Unplug,
  "status-muted": Pause,
  "status-ok": Check,
  "status-slowed": Clock,
  "thumbs-down": ThumbsDown,
  "thumbs-up": ThumbsUp,
} as const;

export type IconName = keyof typeof glyphs;

export function Icon(props: {
  name: IconName;
  size?: IconSize;
  filled?: boolean;
  class?: string;
}) {
  const size = () => props.size ?? 16;
  const fillable = () => props.filled !== undefined;
  const className = () =>
    [
      "icon",
      `icon-${size()}`,
      fillable() ? "icon-fillable" : "",
      props.class ?? "",
    ]
      .filter(Boolean)
      .join(" ");

  return (
    <Dynamic
      component={glyphs[props.name]}
      class={className()}
      size={size()}
      fill={props.filled ? "currentColor" : "none"}
      data-on={fillable() ? String(props.filled) : undefined}
      aria-hidden="true"
    />
  );
}
