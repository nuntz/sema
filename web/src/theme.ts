import { type Accessor, createMemo, createSignal, onCleanup } from "solid-js";

export type ThemePreference = "system" | "light" | "dark";
export type ResolvedTheme = Exclude<ThemePreference, "system">;

export const themeStorageKey = "sema:theme";
export const themeColor = {
  dark: "#0b0c0e",
  light: "#14161a",
} as const satisfies Record<ResolvedTheme, string>;

export interface ThemeController {
  preference: Accessor<ThemePreference>;
  resolvedTheme: Accessor<ResolvedTheme>;
  setPreference(preference: ThemePreference): void;
  cyclePreference(): void;
}

type ThemeEnvironment = {
  window: Pick<Window, "addEventListener" | "removeEventListener"> & {
    matchMedia(query: string): MediaQueryList;
  };
  document: Pick<Document, "documentElement" | "querySelector">;
  storage: Pick<Storage, "getItem" | "setItem" | "removeItem">;
};

const themePreferences: readonly ThemePreference[] = [
  "system",
  "light",
  "dark",
];

export function isThemePreference(value: unknown): value is ThemePreference {
  return themePreferences.includes(value as ThemePreference);
}

export function nextThemePreference(
  preference: ThemePreference,
): ThemePreference {
  const index = themePreferences.indexOf(preference);
  return themePreferences[(index + 1) % themePreferences.length];
}

function readPreference(storage: Pick<Storage, "getItem">): ThemePreference {
  try {
    const stored = storage.getItem(themeStorageKey);
    return isThemePreference(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

function browserEnvironment(): ThemeEnvironment {
  try {
    return { window, document, storage: window.localStorage };
  } catch {
    return {
      window,
      document,
      storage: {
        getItem: () => null,
        setItem: () => undefined,
        removeItem: () => undefined,
      },
    };
  }
}

export function createThemeController(
  environment: ThemeEnvironment = browserEnvironment(),
): ThemeController {
  const media = environment.window.matchMedia("(prefers-color-scheme: light)");
  const [preference, setPreferenceSignal] = createSignal<ThemePreference>(
    readPreference(environment.storage),
  );
  const [systemIsLight, setSystemIsLight] = createSignal(media.matches);
  const resolvedTheme = createMemo<ResolvedTheme>(() => {
    const choice = preference();
    return choice === "system" ? (systemIsLight() ? "light" : "dark") : choice;
  });

  const applyTheme = (choice: ThemePreference, resolved: ResolvedTheme) => {
    const root = environment.document.documentElement;
    if (choice === "system") root.removeAttribute("data-theme");
    else root.setAttribute("data-theme", choice);

    const meta = environment.document.querySelector<HTMLMetaElement>(
      'meta[name="theme-color"]',
    );
    meta?.setAttribute("content", themeColor[resolved]);
  };

  applyTheme(preference(), resolvedTheme());

  const onSystemThemeChange = () => {
    setSystemIsLight(media.matches);
    if (preference() === "system") applyTheme("system", resolvedTheme());
  };
  media.addEventListener("change", onSystemThemeChange);

  const onStorage = (event: StorageEvent) => {
    if (event.key !== themeStorageKey) return;
    const next = isThemePreference(event.newValue) ? event.newValue : "system";
    setPreferenceSignal(next);
    applyTheme(next, resolvedTheme());
  };
  environment.window.addEventListener("storage", onStorage);

  onCleanup(() => {
    media.removeEventListener("change", onSystemThemeChange);
    environment.window.removeEventListener("storage", onStorage);
  });

  const setPreference = (next: ThemePreference) => {
    setPreferenceSignal(next);
    applyTheme(next, resolvedTheme());
    try {
      if (next === "system") environment.storage.removeItem(themeStorageKey);
      else environment.storage.setItem(themeStorageKey, next);
    } catch {
      // The in-memory preference still applies when storage is unavailable.
    }
  };

  return {
    preference,
    resolvedTheme,
    setPreference,
    cyclePreference: () => setPreference(nextThemePreference(preference())),
  };
}
