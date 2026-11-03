// The colour scheme first: it is applied while the rest of the app loads.
import {initTheme} from "./lib/theme";
import {StrictMode} from "react";
import {createRoot} from "react-dom/client";
import App from "./App";
import "./styles.css";

initTheme(); // before the first paint: no flash of the wrong colours

createRoot(document.getElementById("root")!).render(
    <StrictMode>
        <App/>
    </StrictMode>,
);
