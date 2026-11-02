import {createContext, useContext, useState} from "react";
import type {ReactNode} from "react";
import {HashRouter, Navigate, NavLink, Route, Routes} from "react-router";
import {Svc} from "./api";
import type {StatusDTO} from "./api";
import {FeedbackProvider} from "./components/feedback";
import {ErrorNotice, Loading, Notice} from "./components/ui";
import {useBackend} from "./lib/useBackend";
import Dashboard from "./pages/Dashboard";
import Import from "./pages/Import";
import InvoiceForm from "./pages/InvoiceForm";
import Invoices from "./pages/Invoices";
import Overview from "./pages/Overview";
import Settings from "./pages/Settings";
import Setup from "./pages/Setup";

export interface AppState {
    status: StatusDTO;
    reloadStatus: () => void;
    /** Leistungsjahr shown on Dashboard and Rechnungen (kept when switching pages). */
    year: number;
    setYear: (year: number) => void;
}

export const AppContext = createContext<AppState | null>(null);

export function useApp(): AppState {
    const ctx = useContext(AppContext);
    if (!ctx) throw new Error("useApp outside of App");
    return ctx;
}

const NAV = [
    ["/dashboard", "Dashboard"],
    ["/rechnungen", "Rechnungen"],
    ["/rechnung/neu", "Neue Rechnung"],
    ["/jahre", "Alle Jahre"],
    ["/import", "Import"],
    ["/einstellungen", "Einstellungen"],
] as const;

function Layout({status, children}: {status: StatusDTO; children: ReactNode}) {
    return (
        <div className="layout">
            <nav className="sidebar">
                <div className="brand">PKV · Beihilfe</div>
                {NAV.map(([to, label]) => <NavLink key={to} to={to}>{label}</NavLink>)}
                <div className="nav-foot">
                    {status.dataDir && <>Daten: {status.dataDir}<br/></>}
                    Version {status.version}
                </div>
            </nav>
            <main className="content">
                <StatusNotices status={status}/>
                {children}
            </main>
        </div>
    );
}

function StatusNotices({status}: {status: StatusDTO}) {
    const issues = status.issues ?? [];
    const problems = status.problems ?? [];
    return (
        <>
            {status.error && <ErrorNotice error={status.error}/>}
            {issues.length > 0 && (
                <Notice kind="warn">
                    Beim Einlesen des Datenordners:
                    <ul>{issues.map(i => <li key={i.path + i.text}>{i.path}: {i.text}</li>)}</ul>
                </Notice>
            )}
            {problems.length > 0 && (
                <Notice kind={problems.some(p => p.error) ? "bad" : "warn"}>
                    Einstellungen prüfen:
                    <ul>{problems.map(p => <li key={p.text}>{p.text}</li>)}</ul>
                </Notice>
            )}
        </>
    );
}

function AppRoutes({status}: {status: StatusDTO}) {
    if (!status.configured || status.needsSetup) {
        return <Routes><Route path="*" element={<Setup/>}/></Routes>;
    }
    return (
        <Layout status={status}>
            <Routes>
                <Route path="/" element={<Navigate to="/dashboard" replace/>}/>
                <Route path="/dashboard" element={<Dashboard/>}/>
                <Route path="/rechnungen" element={<Invoices/>}/>
                <Route path="/rechnung/neu" element={<InvoiceForm/>}/>
                <Route path="/rechnung/:id" element={<InvoiceForm/>}/>
                <Route path="/jahre" element={<Overview/>}/>
                <Route path="/import" element={<Import/>}/>
                <Route path="/einstellungen" element={<Settings/>}/>
                <Route path="*" element={<Navigate to="/dashboard" replace/>}/>
            </Routes>
        </Layout>
    );
}

export default function App() {
    const {data: status, error, reload} = useBackend(() => Svc.Status(), []);
    const [year, setYear] = useState(new Date().getFullYear());
    return (
        <FeedbackProvider>
            <HashRouter>
                {error && <main className="content"><ErrorNotice error={error}/></main>}
                {!status && !error && <Loading/>}
                {status && (
                    <AppContext.Provider value={{status, reloadStatus: reload, year, setYear}}>
                        <AppRoutes status={status}/>
                    </AppContext.Provider>
                )}
            </HashRouter>
        </FeedbackProvider>
    );
}
