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
  const handlePageShow = (event: PageTransitionEvent) => {
    if (event.persisted) handleFocus();
  };

  documentTarget.addEventListener("visibilitychange", handleVisibility);
  windowTarget.addEventListener("focus", handleFocus);
  windowTarget.addEventListener("pageshow", handlePageShow);
  windowTarget.addEventListener("online", handleFocus);

  return () => {
    documentTarget.removeEventListener("visibilitychange", handleVisibility);
    windowTarget.removeEventListener("focus", handleFocus);
    windowTarget.removeEventListener("pageshow", handlePageShow);
    windowTarget.removeEventListener("online", handleFocus);
  };
}
