import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import ArtistRoute from "./components/ArtistRoute";
import "./styles/lineup.css";
import "./styles/app.css";
import "./styles/ui.css";

// Two routes, one document: `/a/<key>` is an artist's dates, everything else is the board. The Worker serves the
// same index.html for both (assets-first, `single-page-application`), so the split is a pathname test, not a server.
const artist = location.pathname.match(/^\/a\/(.+?)\/?$/);

createRoot(document.getElementById("root")!).render(
  <StrictMode>{artist ? <ArtistRoute artistKey={decodeURIComponent(artist[1])} /> : <App />}</StrictMode>,
);
