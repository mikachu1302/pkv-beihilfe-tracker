import {StrictMode} from "react";
import {createRoot} from "react-dom/client";
import App from "./App";
import {initTheme} from "./lib/theme";
import "./styles.css";

initTheme(); // before the first paint: no flash of the wrong colours

createRoot(document.getElementById("root")!).render(
    <StrictMode>
        <App/>
    </StrictMode>,
);
