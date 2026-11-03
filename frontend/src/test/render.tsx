import {render} from "@testing-library/react";
import type {ReactElement} from "react";
import {MemoryRouter, Route, Routes} from "react-router";
import type {StatusDTO} from "../api";
import {AppContext} from "../App";
import type {AppState} from "../App";
import {FeedbackProvider} from "../components/feedback";

export const testStatus: StatusDTO = {
    configured: true, needsSetup: false, dataDir: "/daten", version: "test", deviceId: "abc123", error: "",
    issues: [], problems: [], warnings: [],
};

/**
 * Renders an element inside router, feedback and app-state providers.
 * setApp changes the app state of the same tree (e.g. a live update raising dataVersion).
 */
export function renderAt(path: string | {pathname: string; state: unknown}, routePattern: string, element: ReactElement, app?: Partial<AppState>) {
    let state: AppState = {status: testStatus, reloadStatus: () => {}, year: 2026, setYear: () => {}, dataVersion: 0, ...app};
    const tree = () => (
        <FeedbackProvider>
            <AppContext.Provider value={state}>
                <MemoryRouter initialEntries={[path]}>
                    <Routes>
                        <Route path={routePattern} element={element}/>
                        <Route path="*" element={<div>andere Seite</div>}/>
                    </Routes>
                </MemoryRouter>
            </AppContext.Provider>
        </FeedbackProvider>
    );
    const result = render(tree());
    const setApp = (patch: Partial<AppState>) => {
        state = {...state, ...patch};
        result.rerender(tree());
    };
    return {...result, setApp};
}
