import {createContext, useContext, useEffect, useState} from "react";
import {Events} from "@wailsio/runtime";
import type {ReactNode} from "react";
import {HashRouter, Navigate, NavLink, Route, Routes} from "react-router";
import {Svc} from "./api";
import type {StatusDTO} from "./api";
import {FeedbackProvider, useToast} from "./components/feedback";
import {ErrorNotice, Loading, Notice} from "./components/ui";
import {useBackend} from "./lib/useBackend";
import Dashboard from "./pages/Dashboard";
import Import from "./pages/Import";
import Inbox from "./pages/Inbox";
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
    /** Increases whenever the data folder was changed from outside (another device). */
    dataVersion: number;
}

export const AppContext = createContext<AppState | null>(null);

export function useApp(): AppState {
    const ctx = useContext(AppContext);
    if (!ctx) throw new Error("useApp outside of App");
    return ctx;
}

const InboxChangedContext = createContext<() => void>(() => {});

/** Refreshes the Posteingang count in the navigation after own changes to the inbox. */
export function useInboxChanged(): () => void {
    return useContext(InboxChangedContext);
}

const NAV = [
    ["/dashboard", "Dashboard"],
    ["/posteingang", "Posteingang"],
    ["/rechnungen", "Rechnungen"],
    ["/rechnung/neu", "Neue Rechnung"],
    ["/jahre", "Alle Jahre"],
    ["/import", "Import"],
    ["/einstellungen", "Einstellungen"],
] as const;

function Layout({status, children}: {status: StatusDTO; children: ReactNode}) {
    const {dataVersion} = useApp();
    const inbox = useBackend(() => Svc.ListInbox(), [dataVersion]);
    const waiting = inbox.data?.length ?? 0;
    return (
        <div className="layout">
            <nav className="sidebar">
                <div className="brand">PKV · Beihilfe</div>
                {NAV.map(([to, label]) => (
                    <NavLink key={to} to={to}>
                        {label}{to === "/posteingang" && waiting > 0 && <span className="nav-count" aria-label={`${waiting} Belege`}>{waiting}</span>}
                    </NavLink>
                ))}
                <div className="nav-foot">
                    {status.dataDir && <>Daten: {status.dataDir}<br/></>}
                    Version {status.version}
                </div>
            </nav>
            <main className="content">
                <StatusNotices status={status}/>
                <InboxChangedContext.Provider value={inbox.reload}>{children}</InboxChangedContext.Provider>
            </main>
        </div>
    );
}

function StatusNotices({status}: {status: StatusDTO}) {
    const issues = status.issues ?? [];
    const problems = status.problems ?? [];
    const warnings = status.warnings ?? [];
    return (
        <>
            {status.error && <ErrorNotice error={status.error}/>}
            {warnings.map(w => <Notice key={w} kind="warn">{w}</Notice>)}
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
                <Route path="/posteingang" element={<Inbox/>}/>
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

/** Listens for changes another device made to the data folder. */
function LiveUpdates({onChange}: {onChange: () => void}) {
    const toast = useToast();
    useEffect(() => Events.On("data-changed", ev => {
        const d = ev.data as {issues?: number; inboxAdded?: number; error?: string} | undefined;
        if (d?.error) toast.show(`Datenordner konnte nicht neu eingelesen werden: ${d.error}`, true);
        else if (d?.inboxAdded) toast.show(d.inboxAdded === 1 ? "Neuer Beleg im Posteingang" : `${d.inboxAdded} neue Belege im Posteingang`);
        else toast.show(d?.issues ? "Änderungen übernommen – bitte die Hinweise oben beachten" : "Änderungen aus dem Datenordner übernommen");
        onChange();
    }), [toast, onChange]);
    // Dropping a file outside the inbox must not navigate the window away.
    useEffect(() => {
        const stop = (e: DragEvent) => e.preventDefault();
        window.addEventListener("dragover", stop);
        window.addEventListener("drop", stop);
        return () => {
            window.removeEventListener("dragover", stop);
            window.removeEventListener("drop", stop);
        };
    }, []);
    return null;
}

export default function App() {
    const {data: status, error, reload} = useBackend(() => Svc.Status(), []);
    const [year, setYear] = useState(new Date().getFullYear());
    const [dataVersion, setDataVersion] = useState(0);
    const [onChange] = useState(() => () => {
        setDataVersion(v => v + 1);
        reload();
    });
    return (
        <FeedbackProvider>
            <LiveUpdates onChange={onChange}/>
            <HashRouter>
                {error && <main className="content"><ErrorNotice error={error}/></main>}
                {!status && !error && <Loading/>}
                {status && (
                    <AppContext.Provider value={{status, reloadStatus: reload, year, setYear, dataVersion}}>
                        <AppRoutes status={status}/>
                    </AppContext.Provider>
                )}
            </HashRouter>
        </FeedbackProvider>
    );
}
