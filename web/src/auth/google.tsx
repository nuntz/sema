import { createSignal, type JSX, onCleanup, onMount, Show } from "solid-js";
import { Icon } from "../components/Icon";

declare global {
  interface Window {
    google?: {
      accounts: {
        id: {
          initialize(config: {
            client_id: string;
            callback: (response: { credential: string }) => void;
            auto_select?: boolean;
          }): void;
          renderButton(
            element: HTMLElement,
            options: Record<string, unknown>,
          ): void;
          prompt(
            callback?: (notification: { isNotDisplayed(): boolean }) => void,
          ): void;
          disableAutoSelect(): void;
        };
      };
    };
  }
}

const tokenKey = "sema.google-token";

function expiration(token: string): number {
  try {
    const payload = JSON.parse(
      atob(token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/")),
    );
    return Number(payload.exp ?? 0) * 1000;
  } catch {
    return 0;
  }
}

function valid(token: string): boolean {
  return expiration(token) > Date.now() + 30_000;
}

export function AuthGate(props: {
  children: (token: () => string, signOut: () => void) => JSX.Element;
}) {
  const stored = sessionStorage.getItem(tokenKey) ?? "";
  const [token, setToken] = createSignal(valid(stored) ? stored : "");
  const [ready, setReady] = createSignal(false);
  const [error, setError] = createSignal("");
  let button: HTMLDivElement | undefined;
  let refreshTimer: number | undefined;

  const accept = (credential: string) => {
    if (!valid(credential)) {
      setError("Google returned an expired sign-in token.");
      return;
    }
    sessionStorage.setItem(tokenKey, credential);
    setToken(credential);
    scheduleRefresh(credential);
  };

  const scheduleRefresh = (credential: string) => {
    window.clearTimeout(refreshTimer);
    const delay = Math.max(
      1_000,
      expiration(credential) - Date.now() - 5 * 60_000,
    );
    refreshTimer = window.setTimeout(
      () => window.google?.accounts.id.prompt(),
      delay,
    );
  };

  const initialize = () => {
    const clientID = import.meta.env.VITE_GOOGLE_CLIENT_ID;
    if (!clientID) {
      setError("VITE_GOOGLE_CLIENT_ID is not configured.");
      setReady(true);
      return;
    }
    if (!window.google) return;
    window.google.accounts.id.initialize({
      client_id: clientID,
      callback: (response) => accept(response.credential),
      auto_select: true,
    });
    if (button)
      window.google.accounts.id.renderButton(button, {
        theme: "filled_black",
        size: "large",
        shape: "rectangular",
        width: 260,
      });
    setReady(true);
    if (token()) scheduleRefresh(token());
    else window.google.accounts.id.prompt();
  };

  onMount(() => {
    const script = document.createElement("script");
    script.src = "https://accounts.google.com/gsi/client";
    script.async = true;
    script.onload = initialize;
    script.onerror = () => {
      setError("Google Sign-In could not be loaded.");
      setReady(true);
    };
    document.head.append(script);
    onCleanup(() => script.remove());
  });
  onCleanup(() => window.clearTimeout(refreshTimer));

  const signOut = () => {
    sessionStorage.removeItem(tokenKey);
    window.google?.accounts.id.disableAutoSelect();
    setToken("");
    queueMicrotask(initialize);
  };

  return (
    <Show
      when={token()}
      fallback={
        <main class="signin-shell">
          <section class="signin-card">
            <div class="signin-mark">
              <Icon name="back-to-grid" size={24} class="icon-quiet" />
            </div>
            <h1>Sema</h1>
            <p>Your feed, shaped around what deserves your attention.</p>
            <div ref={button} class="google-button" aria-busy={!ready()} />
            <Show when={error()}>
              <p class="form-error" role="alert">
                {error()}
              </p>
            </Show>
          </section>
        </main>
      }
    >
      {props.children(token, signOut)}
    </Show>
  );
}
