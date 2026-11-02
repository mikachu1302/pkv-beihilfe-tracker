import {render} from "@testing-library/react";
import type {ReactElement} from "react";
import {MemoryRouter, Route, Routes} from "react-router";
import type {StatusDTO} from "../api";
import {AppContext} from "../App";
import type {AppState} from "../App";
import {FeedbackProvider} from "../components/feedback";

export const testStatus: StatusDTO = {
    configured: true, needsSetup: false, dataDir: "/daten", version: "test", deviceId: "abc123", error: "",
    issues: [], problems: [],
};

/** Renders an element inside router, feedback and app-state providers. */
export function renderAt(path: string, routePattern: string, element: ReactElement, app?: Partial<AppState>) {
    const state: AppState = {status: testStatus, reloadStatus: () => {}, year: 2026, setYear: () => {}, ...app};
    return render(
        <FeedbackProvider>
            <AppContext.Provider value={state}>
                <MemoryRouter initialEntries={[path]}>
                    <Routes>
                        <Route path={routePattern} element={element}/>
                        <Route path="*" element={<div>andere Seite</div>}/>
                    </Routes>
                </MemoryRouter>
            </AppContext.Provider>
        </FeedbackProvider>,
    );
}
