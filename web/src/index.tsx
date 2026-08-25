import { render } from "solid-js/web";
import { App } from "./App";
import { AuthGate } from "./auth/google";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("Sema root element is missing");

render(
  () => <AuthGate>{(signOut) => <App signOut={signOut} />}</AuthGate>,
  root,
);

if ("serviceWorker" in navigator && import.meta.env.PROD)
  navigator.serviceWorker.register("/sw.js");
