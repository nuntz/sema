import Archive from "lucide-solid/icons/archive";
import ArrowDown from "lucide-solid/icons/arrow-down";
import ArrowLeft from "lucide-solid/icons/arrow-left";
import ArrowUp from "lucide-solid/icons/arrow-up";
import ArrowUpDown from "lucide-solid/icons/arrow-up-down";
import Check from "lucide-solid/icons/check";
import ChevronDown from "lucide-solid/icons/chevron-down";
import ChevronLeft from "lucide-solid/icons/chevron-left";
import ChevronRight from "lucide-solid/icons/chevron-right";
import Clock from "lucide-solid/icons/clock";
import Download from "lucide-solid/icons/download";
import Ellipsis from "lucide-solid/icons/ellipsis";
import ExternalLink from "lucide-solid/icons/external-link";
import Heart from "lucide-solid/icons/heart";
import Link from "lucide-solid/icons/link";
import Menu from "lucide-solid/icons/menu";
import Pause from "lucide-solid/icons/pause";
import Play from "lucide-solid/icons/play";
import Plus from "lucide-solid/icons/plus";
import RotateCw from "lucide-solid/icons/rotate-cw";
import Search from "lucide-solid/icons/search";
import Settings from "lucide-solid/icons/settings";
import Tag from "lucide-solid/icons/tag";
import Trash2 from "lucide-solid/icons/trash-2";
import Unplug from "lucide-solid/icons/unplug";
import Upload from "lucide-solid/icons/upload";
import X from "lucide-solid/icons/x";
import { type JSX, splitProps } from "solid-js";
import { Dynamic } from "solid-js/web";

export type IconSize = 12 | 13 | 14 | 15 | 16 | 18 | 20 | 24;

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
  "back-to-grid": ArrowLeft,
  boost: ArrowUp,
  bury: ArrowDown,
  check: Check,
  close: X,
  "copy-link": Link,
  "export-opml": Download,
  "feed-fallback": SemaRows,
  "import-opml": Upload,
  keep: Heart,
  "chevron-down": ChevronDown,
  menu: Menu,
  more: Ellipsis,
  mute: Pause,
  "next-item": ChevronRight,
  "open-original": ExternalLink,
  "previous-item": ChevronLeft,
  play: Play,
  "remove-feed": Trash2,
  retry: RotateCw,
  search: Search,
  settings: Settings,
  sort: ArrowUpDown,
  "status-broken": Unplug,
  "status-muted": Pause,
  "status-ok": Check,
  "status-slowed": Clock,
  tag: Tag,
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
