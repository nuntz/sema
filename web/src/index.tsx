import { render } from "solid-js/web";
import { App } from "./App";
import { AuthGate } from "./auth/google";
import { createThemeController } from "./theme";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("Sema root element is missing");

render(() => {
  const theme = createThemeController();
  return (
    <AuthGate>{(signOut) => <App signOut={signOut} theme={theme} />}</AuthGate>
  );
}, root);

if ("serviceWorker" in navigator && import.meta.env.PROD)
  navigator.serviceWorker.register("/sw.js");
