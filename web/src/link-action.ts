export interface LinkTarget {
  url: string;
  title: string;
}

export interface LinkCapabilities {
  touch: boolean;
  share?: (data: ShareData) => Promise<void>;
  writeText?: (value: string) => Promise<void>;
}

export type LinkAction = "copied" | "shared";

export class LinkActionFailure extends Error {
  constructor(
    readonly action: LinkAction,
    readonly reason: unknown,
  ) {
    super(action === "shared" ? "share failed" : "copy failed");
  }
}

export async function copyOriginalLink(
  target: LinkTarget,
  capabilities = browserCapabilities(),
): Promise<LinkAction> {
  if (capabilities.touch && capabilities.share) {
    try {
      await capabilities.share({ url: target.url, title: target.title });
      return "shared";
    } catch (error) {
      throw new LinkActionFailure("shared", error);
    }
  }
  if (!capabilities.writeText)
    throw new LinkActionFailure("copied", new Error("clipboard unavailable"));
  try {
    await capabilities.writeText(target.url);
    return "copied";
  } catch (error) {
    throw new LinkActionFailure("copied", error);
  }
}

function browserCapabilities(): LinkCapabilities {
  return {
    touch:
      navigator.maxTouchPoints > 0 ||
      window.matchMedia?.("(pointer: coarse)").matches === true,
    share:
      typeof navigator.share === "function"
        ? navigator.share.bind(navigator)
        : undefined,
    writeText: navigator.clipboard?.writeText?.bind(navigator.clipboard),
  };
}

export function isCancelledShare(error: unknown): boolean {
  return (
    error instanceof LinkActionFailure &&
    error.action === "shared" &&
    error.reason instanceof Error &&
    error.reason.name === "AbortError"
  );
}
