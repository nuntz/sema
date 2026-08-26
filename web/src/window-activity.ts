export function listenForWindowReturn(
  onHidden: () => void,
  onReturn: () => void,
  documentTarget: Document = document,
  windowTarget: Window = window,
): () => void {
  const handleVisibility = () => {
    if (documentTarget.visibilityState === "hidden") onHidden();
    else onReturn();
  };
  const handleFocus = () => {
    if (documentTarget.visibilityState === "visible") onReturn();
  };

  documentTarget.addEventListener("visibilitychange", handleVisibility);
  windowTarget.addEventListener("focus", handleFocus);

  return () => {
    documentTarget.removeEventListener("visibilitychange", handleVisibility);
    windowTarget.removeEventListener("focus", handleFocus);
  };
}
