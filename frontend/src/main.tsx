// Colour scheme and design first: they are applied while the rest of the app loads.
import {initTheme} from "./lib/theme";
import "./lib/design";
import {StrictMode} from "react";
import {createRoot} from "react-dom/client";
import App from "./App";
import "./styles/fonts.css";
import "./styles/classic.css";
import "./styles/base.css";
import "./styles/new.css";
import "./styles/views.css";

initTheme(); // before the first paint: no flash of the wrong colours

createRoot(document.getElementById("root")!).render(
    <StrictMode>
        <App/>
    </StrictMode>,
);
