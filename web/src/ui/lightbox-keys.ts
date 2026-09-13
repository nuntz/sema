export const lightboxBindings = [
  ["← / k · → / j", "Previous / next image"],
  ["Escape", "Close lightbox"],
  ["z", "Toggle fit / 1:1"],
  ["Home / End", "First / last image"],
  ["Enter", "Open image original"],
  ["Tab / Shift+Tab", "Cycle lightbox controls"],
  ["?", "Show / hide this card"],
];
export function lightboxCommand(key: string) {
  switch (key) {
    case "ArrowLeft":
    case "k":
      return "previous";
    case "ArrowRight":
    case "j":
      return "next";
    case "Escape":
      return "close";
    case "Home":
      return "first";
    case "End":
      return "last";
    case "z":
      return "zoom";
    case "o":
      return "reader";
    case "Enter":
      return "original";
    case "?":
      return "help";
    case "Tab":
      return "tab";
    default:
      return "swallow";
  }
}
export function imageIndex(index: number, delta: number, count: number) {
  return Math.max(0, Math.min(count - 1, index + delta));
}
