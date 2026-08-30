import type { ThemeController, ThemePreference } from "../theme";
import { nextThemePreference } from "../theme";
import { Icon, type IconName } from "./Icon";
import { Tooltip } from "./Tooltip";

const themeIcons = {
  system: "theme-system",
  light: "theme-light",
  dark: "theme-dark",
} as const satisfies Record<ThemePreference, IconName>;

export function themeToggleLabel(preference: ThemePreference): string {
  return `Theme: ${preference} — switch to ${nextThemePreference(preference)}`;
}

export function ThemeToggle(props: {
  theme: ThemeController;
  tooltipDisabled?: boolean;
}) {
  const label = () => themeToggleLabel(props.theme.preference());

  return (
    <Tooltip name={label()} disabled={props.tooltipDisabled}>
      <button
        type="button"
        class="chrome-icon header-icon-button theme-toggle"
        data-theme-choice={props.theme.preference()}
        aria-label={label()}
        onClick={props.theme.cyclePreference}
      >
        <Icon name={themeIcons[props.theme.preference()]} size={14} />
      </button>
    </Tooltip>
  );
}
