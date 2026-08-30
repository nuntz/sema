import { createRoot } from "solid-js";
import { describe, expect, it, vi } from "vitest";
import {
  createThemeController,
  nextThemePreference,
  themeColor,
  themeStorageKey,
} from "./theme";

class FakeMediaQuery extends EventTarget {
  matches: boolean;
  readonly media = "(prefers-color-scheme: light)";
  readonly onchange = null;

  constructor(matches: boolean) {
    super();
    this.matches = matches;
  }

  setMatches(matches: boolean) {
    this.matches = matches;
    this.dispatchEvent(new Event("change"));
  }

  dispatchEvent(event: Event): boolean {
    return super.dispatchEvent(event);
  }
}

function setup(options: { stored?: string; systemLight?: boolean } = {}) {
  const values = new Map<string, string>();
  if (options.stored !== undefined) values.set(themeStorageKey, options.stored);
  const storage = {
    getItem: vi.fn((key: string) => values.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => values.set(key, value)),
    removeItem: vi.fn((key: string) => values.delete(key)),
  };
  const attributes = new Map<string, string>();
  const root = {
    setAttribute: vi.fn((name: string, value: string) =>
      attributes.set(name, value),
    ),
    removeAttribute: vi.fn((name: string) => attributes.delete(name)),
  };
  let metaContent = "";
  const meta = {
    setAttribute: vi.fn((name: string, value: string) => {
      if (name === "content") metaContent = value;
    }),
  };
  const media = new FakeMediaQuery(options.systemLight ?? false);
  const windowTarget = new EventTarget();
  const environment = {
    window: Object.assign(windowTarget, {
      matchMedia: vi.fn(() => media as unknown as MediaQueryList),
    }) as unknown as Window,
    document: {
      documentElement: root,
      querySelector: vi.fn(() => meta),
    } as unknown as Document,
    storage,
  };
  let controller!: ReturnType<typeof createThemeController>;
  let dispose: () => void = () => undefined;
  createRoot((rootDispose) => {
    dispose = rootDispose;
    controller = createThemeController(environment);
  });

  return {
    attributes,
    controller,
    dispose,
    media,
    metaContent: () => metaContent,
    storage,
    values,
    windowTarget,
  };
}

describe("theme controller", () => {
  it("uses the resolved masthead color for browser chrome", () => {
    expect(themeColor).toEqual({ dark: "#0b0c0e", light: "#14161a" });
  });

  it("follows system theme changes live when no override is stored", () => {
    const context = setup();

    expect(context.controller.preference()).toBe("system");
    expect(context.controller.resolvedTheme()).toBe("dark");
    expect(context.attributes.has("data-theme")).toBe(false);
    expect(context.metaContent()).toBe(themeColor.dark);

    context.media.setMatches(true);
    expect(context.controller.resolvedTheme()).toBe("light");
    expect(context.attributes.has("data-theme")).toBe(false);
    expect(context.metaContent()).toBe(themeColor.light);

    context.dispose();
  });

  it("keeps an explicit override when the system theme changes", () => {
    const context = setup({ stored: "dark", systemLight: true });

    expect(context.controller.preference()).toBe("dark");
    expect(context.controller.resolvedTheme()).toBe("dark");
    expect(context.attributes.get("data-theme")).toBe("dark");

    context.media.setMatches(false);
    expect(context.controller.resolvedTheme()).toBe("dark");
    expect(context.metaContent()).toBe(themeColor.dark);

    context.dispose();
  });

  it("cycles system to light to dark and back to system", () => {
    expect(nextThemePreference("system")).toBe("light");
    expect(nextThemePreference("light")).toBe("dark");
    expect(nextThemePreference("dark")).toBe("system");

    const context = setup();
    context.controller.cyclePreference();
    expect(context.controller.preference()).toBe("light");
    context.controller.cyclePreference();
    expect(context.controller.preference()).toBe("dark");
    context.controller.cyclePreference();
    expect(context.controller.preference()).toBe("system");

    context.dispose();
  });

  it("persists overrides and removes storage for system mode", () => {
    const context = setup();

    context.controller.setPreference("light");
    expect(context.storage.setItem).toHaveBeenLastCalledWith(
      themeStorageKey,
      "light",
    );
    expect(context.values.get(themeStorageKey)).toBe("light");
    expect(context.attributes.get("data-theme")).toBe("light");

    context.controller.setPreference("system");
    expect(context.storage.removeItem).toHaveBeenLastCalledWith(
      themeStorageKey,
    );
    expect(context.values.has(themeStorageKey)).toBe(false);
    expect(context.attributes.has("data-theme")).toBe(false);

    context.dispose();
  });

  it("synchronizes the theme-color meta tag with the resolved theme", () => {
    const context = setup({ systemLight: true });
    expect(context.metaContent()).toBe(themeColor.light);

    context.controller.setPreference("dark");
    expect(context.metaContent()).toBe(themeColor.dark);

    context.controller.setPreference("system");
    expect(context.metaContent()).toBe(themeColor.light);

    context.dispose();
  });
});
