import { mount } from "svelte";
import "./lib/theme.css";
import App from "./App.svelte";
import { initSessionFromUrl } from "./lib/session";

const target = document.getElementById("app");
if (!target) {
  throw new Error("main: #app element not found");
}

// Bootstrap the authenticated mutation session (if a one-time
// `?bootstrap=` token is present) before mounting, so App.svelte and any
// review route never race the exchange. Errors are swallowed here - a
// failed/missing exchange just means mutating requests will 401/403
// later, which ReviewMode/StartReview surface as SessionExpiredError.
void initSessionFromUrl().finally(() => {
  mount(App, { target });
});
