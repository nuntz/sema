import { createSignal, type JSX, onCleanup, onMount, Show } from "solid-js";
import { clearSessionBootstrap, primeSessionBootstrap } from "../api/client";
import type { MeResponse } from "../types";

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
          prompt(): void;
          disableAutoSelect(): void;
        };
      };
    };
  }
}

export const signedInKey = "sema.signed-in";

function hasSessionFlag(): boolean {
  try {
    return localStorage.getItem(signedInKey) === "1";
  } catch {
    return false;
  }
}

export async function signInWithCredential(
  credential: string,
  storage?: Pick<Storage, "setItem">,
): Promise<void> {
  const response = await fetch("/api/session", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ credential }),
  });
  if (!response.ok) throw new Error("Google Sign-In was not accepted.");
  const bootstrap = (await response.json()) as MeResponse;
  try {
    (storage ?? localStorage).setItem(signedInKey, "1");
  } catch {
    // The cookie remains authoritative when persistent browser storage is unavailable.
  }
  primeSessionBootstrap(bootstrap);
}

export async function deleteSession(
  storage?: Pick<Storage, "removeItem">,
): Promise<void> {
  try {
    (storage ?? localStorage).removeItem(signedInKey);
  } catch {
    // The in-memory state below still returns the current page to sign-in.
  }
  clearSessionBootstrap();
  const response = await fetch("/api/session", { method: "DELETE" });
  if (!response.ok && response.status !== 401)
    throw new Error("Sema could not end the session.");
}

export function AuthGate(props: {
  children: (signOut: () => void) => JSX.Element;
}) {
  const [signedIn, setSignedIn] = createSignal(hasSessionFlag());
  const [ready, setReady] = createSignal(false);
  const [error, setError] = createSignal("");
  let button: HTMLDivElement | undefined;

  const accept = async (credential: string) => {
    setError("");
    try {
      await signInWithCredential(credential);
      setSignedIn(true);
    } catch (caught) {
      setError(
        caught instanceof Error ? caught.message : "Google Sign-In failed.",
      );
    }
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
      callback: (response) => void accept(response.credential),
      auto_select: true,
    });
    if (button && !signedIn())
      window.google.accounts.id.renderButton(button, {
        theme: "filled_black",
        size: "large",
        shape: "rectangular",
        width: 260,
      });
    setReady(true);
    if (!signedIn()) window.google.accounts.id.prompt();
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

  const signOut = () => {
    window.google?.accounts.id.disableAutoSelect();
    setSignedIn(false);
    setReady(false);
    setError("");
    void deleteSession()
      .catch(() => undefined)
      .finally(() => queueMicrotask(initialize));
  };

  return (
    <Show
      when={signedIn()}
      fallback={
        <main class="signin-shell">
          <section class="signin-card">
            <div class="signin-mark">
              <img src="/sema-mark.svg" alt="" aria-hidden="true" />
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
      {props.children(signOut)}
    </Show>
  );
}
