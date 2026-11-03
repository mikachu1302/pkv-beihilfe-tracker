import {createContext, useContext, useEffect, useState} from "react";
import {Events} from "@wailsio/runtime";
import type {ReactNode} from "react";
import {HashRouter, Navigate, NavLink, Route, Routes, useLocation, useNavigate} from "react-router";
import {Svc} from "./api";
import type {StatusDTO} from "./api";
import {FeedbackProvider, useConfirm, useToast} from "./components/feedback";
import {ErrorNotice, Loading, Notice} from "./components/ui";
import {setAI, useAIOn} from "./lib/ai";
import {setAppearance, useAppearance} from "./lib/appearance";
import {useCloseDesignHint, useSetDesign, useSetTheme} from "./lib/appearanceActions";
import {applyDesign, useDesign} from "./lib/design";
import {loadLocal, saveLocal} from "./lib/store";
import {applyTheme, useDarkActive} from "./lib/theme";
import {useBackend} from "./lib/useBackend";
import {clearUnsavedChanges, dialogOpen, hasUnsavedChanges, isMac, isTyping, key, mod, useKeydown} from "./lib/shortcuts";
import {useDialog} from "./lib/useDialog";
import Ask from "./pages/Ask";
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
    ["/dashboard", "Dashboard", "dashboard"],
    ["/posteingang", "Posteingang", "inbox"],
    ["/rechnungen", "Rechnungen", "list"],
    ["/rechnung/neu", "Neue Rechnung", "plus"],
    ["/jahre", "Alle Jahre", "chart"],
    ["/fragen", "Fragen", "ask"],
    ["/import", "Import", "import"],
    ["/einstellungen", "Einstellungen", "settings"],
] as const;

/** The menu entries: "Fragen" only while the AI is on. */
function useNav() {
    const aiOn = useAIOn();
    return NAV.filter(([to]) => to !== "/fragen" || aiOn);
}

const NAV_KEY = "pkv.navCollapsed";

/**
 * Whether the menu is collapsed to icons; remembered on this device. Until
 * it is chosen, the new design starts with the icon rail, the classic one
 * with the full menu.
 */
function useNavCollapsed(): [boolean, (v: boolean) => void] {
    const design = useDesign();
    const [chosen, setChosen] = useState<boolean | null>(() => {
        const v = loadLocal(NAV_KEY);
        return v === null ? null : v === "1";
    });
    const set = (v: boolean) => {
        setChosen(v);
        saveLocal(NAV_KEY, v ? "1" : "0");
    };
    return [chosen ?? design === "new", set];
}

function Layout({status, children}: {status: StatusDTO; children: ReactNode}) {
    const {dataVersion} = useApp();
    const nav = useNav();
    const inbox = useBackend(() => Svc.ListInbox(), [dataVersion]);
    const waiting = inbox.data?.length ?? 0;
    const [collapsed, setCollapsed] = useNavCollapsed();
    const [help, setHelp] = useState(false);
    const navigate = useNavigate();
    const confirm = useConfirm();
    return (
        <div className={`layout ${collapsed ? "nav-collapsed" : ""}`}>
            <nav className="sidebar" aria-label="Hauptmenü">
                <div className="brand">
                    <span className="logo" aria-hidden="true">P·B</span>
                    <span className="brand-text">PKV · Beihilfe</span>
                    <button type="button" className="nav-toggle" aria-expanded={!collapsed}
                            aria-label={collapsed ? "Menü ausklappen" : "Menü einklappen"}
                            title={collapsed ? "Menü ausklappen" : "Menü einklappen"}
                            onClick={() => setCollapsed(!collapsed)}>
                        <Icon name={collapsed ? "expand" : "collapse"}/>
                    </button>
                </div>
                {nav.map(([to, label, icon]) => (
                    <NavLink key={to} to={to} title={collapsed ? label : undefined}
                             aria-label={collapsed ? (to === "/posteingang" && waiting > 0 ? `${label}, ${waiting} Belege` : label) : undefined}
                             onClick={async e => {
                                 // Leaving a form with unsaved changes asks first (as the shortcuts do).
                                 if (!hasUnsavedChanges()) return;
                                 e.preventDefault();
                                 if (await confirm("Die Änderungen sind noch nicht gespeichert. Trotzdem verlassen?", "Verlassen")) {
                                     clearUnsavedChanges();
                                     navigate(to);
                                 }
                             }}>
                        <Icon name={icon}/>
                        <span className="nav-label">{label}</span>
                        {to === "/posteingang" && waiting > 0 && <span className="nav-count" aria-label={`${waiting} Belege`}>{waiting}</span>}
                    </NavLink>
                ))}
                <ThemeToggle/>
                <div className="nav-foot">
                    {status.dataDir && <>Daten: {status.dataDir}<br/></>}
                    Version {status.version}
                    <br/><button type="button" className="link nav-help" onClick={() => setHelp(true)}>Tastenkürzel (?)</button>
                </div>
            </nav>
            <GlobalShortcuts onHelp={() => setHelp(true)}/>
            {help && <ShortcutHelp onClose={() => setHelp(false)}/>}
            <main className="content">
                <DesignHint/>
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
                <Route path="/rechnung/neu" element={<NewInvoiceForm/>}/>
                <Route path="/rechnung/:id" element={<InvoiceForm/>}/>
                <Route path="/jahre" element={<Overview/>}/>
                <Route path="/fragen" element={<Ask/>}/>
                <Route path="/import" element={<Import/>}/>
                <Route path="/einstellungen" element={<Settings/>}/>
                <Route path="*" element={<Navigate to="/dashboard" replace/>}/>
            </Routes>
        </Layout>
    );
}

/**
 * Switch "Dunkel" in the menu: dark mode on or off. It shows the scheme in effect
 * (also with "Automatisch"); switching chooses light or dark for this device.
 */
function ThemeToggle() {
    const dark = useDarkActive();
    const setTheme = useSetTheme();
    return (
        <button type="button" role="switch" aria-checked={dark} aria-label="Dunkel" title="Dunkel"
                className="nav-button theme-switch" onClick={() => setTheme(dark ? "light" : "dark")}>
            <Icon name={dark ? "theme-dark" : "theme-light"}/>
            <span className="nav-label">Dunkel</span>
            <span className="switch" aria-hidden="true"/>
        </button>
    );
}

/**
 * Takes the settings of this device from the device configuration: colour
 * scheme and design (localStorage, which painted the first frame, may be
 * empty or from an older version) and the AI settings.
 */
function DeviceSync({dataDir}: {dataDir?: string}) {
    // again after the data folder was set: a device set up now gets no hint about the old design
    useEffect(() => {
        let live = true; // a late answer of an earlier load must not undo a newer one
        Svc.Appearance().then(a => {
            if (!a || !live) return;
            setAppearance(a);
            applyTheme(a.theme);
            applyDesign(a.design);
        }, () => {});
        return () => {
            live = false;
        };
    }, [dataDir]);
    useEffect(() => {
        Svc.AISettings().then(a => a && setAI(a), () => {});
    }, []);
    return null;
}

/** Once after the update to v0.4.0: where to switch back to the classic design. */
function DesignHint() {
    const appearance = useAppearance();
    const design = useDesign();
    const setDesign = useSetDesign();
    const close = useCloseDesignHint();
    if (!appearance?.designHint || design !== "new") return null;
    return (
        <Notice kind="good">
            <div className="row">
                <span className="grow">
                    <strong>Neues Aussehen.</strong> Das bisherige Design gibt es weiterhin: unter Einstellungen → Darstellung → „Klassisch“.
                </span>
                <button onClick={() => setDesign("classic")}>Klassisch verwenden</button>
                <button className="primary" onClick={close}>Verstanden</button>
            </div>
        </Notice>
    );
}

/** Listens for changes another device made to the data folder. */
function LiveUpdates({onChange}: {onChange: () => void}) {
    const toast = useToast();
    useEffect(() => Events.On("data-changed", ev => {
        const d = ev.data as {issues?: number; inboxAdded?: number; inboxPages?: number; error?: string} | undefined;
        if (d?.error) toast.show(`Datenordner konnte nicht neu eingelesen werden: ${d.error}`, true);
        else if (d?.inboxAdded) toast.show(d.inboxAdded === 1 ? "Neuer Beleg im Posteingang" : `${d.inboxAdded} neue Belege im Posteingang`);
        else if (d?.inboxPages) toast.show(d.inboxPages === 1 ? "Neue Seite im Posteingang" : `${d.inboxPages} neue Seiten im Posteingang`);
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
            <DeviceSync dataDir={status?.dataDir}/>
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

const ICONS: Record<string, ReactNode> = {
    dashboard: <><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></>,
    inbox: <><path d="M3 13h5l1.5 3h5L16 13h5"/><path d="M5.5 5h13L21 13v6H3v-6z"/></>,
    list: <><path d="M8 6h13M8 12h13M8 18h13"/><path d="M3.5 6h.01M3.5 12h.01M3.5 18h.01"/></>,
    plus: <><circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/></>,
    chart: <><path d="M4 20V10M10 20V4M16 20v-7M22 20H2"/></>,
    import: <><path d="M12 3v12M7 10l5 5 5-5"/><path d="M4 17v3h16v-3"/></>,
    settings: <><path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0"/><circle cx="16" cy="6" r="2"/><circle cx="10" cy="12" r="2"/><circle cx="18" cy="18" r="2"/></>,
    collapse: <path d="M15 6l-6 6 6 6"/>,
    expand: <path d="M9 6l6 6-6 6"/>,
    "theme-light": <><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></>,
    "theme-dark": <path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/>,
    ask: <><path d="M21 12a8.5 8.5 0 0 1-12.3 7.6L3.5 21l1.4-4.6A8.5 8.5 0 1 1 21 12z"/><path d="M9.8 9.5a2.3 2.3 0 1 1 3 2.2c-.5.2-.8.6-.8 1.1v.4M12 16h.01"/></>,
};

/** Small line icons for the menu (inline SVG, colour from the text). */
function Icon({name}: {name: string}) {
    return (
        <svg className="icon" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.8"
             strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{ICONS[name]}</svg>
    );
}

/**
 * Shortcuts that work on every page: back and forward, new invoice, the menu
 * entries, and the list of shortcuts. Leaving a form with unsaved changes asks first.
 */
function GlobalShortcuts({onHelp}: {onHelp: () => void}) {
    const navigate = useNavigate();
    const nav = useNav();
    const confirm = useConfirm();
    const leave = async (go: () => void) => {
        if (hasUnsavedChanges()) {
            if (!await confirm("Die Änderungen sind noch nicht gespeichert. Trotzdem verlassen?", "Verlassen")) return;
            clearUnsavedChanges();
        }
        go();
    };
    useKeydown(e => {
        if (dialogOpen()) return;
        const typing = isTyping(e.target);
        const plain = !e.ctrlKey && !e.metaKey && !e.altKey;
        // Alt+arrows only outside text fields and not on macOS, where Option+arrow moves by words.
        const altArrow = (k: string) => !isMac && !typing && e.altKey && !e.ctrlKey && !e.metaKey && e.key === k;
        const idx = ((window.history.state as {idx?: number} | null)?.idx ?? 0);
        if ((e.key === "Backspace" && plain && !typing) || altArrow("ArrowLeft") || (isMac && e.metaKey && e.key === "[")) {
            // react-router keeps the position in the history: never leave the app
            if (idx > 0) void leave(() => navigate(-1));
            return true;
        }
        if (altArrow("ArrowRight") || (isMac && e.metaKey && e.key === "]")) {
            if (idx < window.history.length - 1) void leave(() => navigate(1));
            return true;
        }
        if (mod(e) && !e.shiftKey && e.key.toLowerCase() === "n") {
            void leave(() => navigate("/rechnung/neu"));
            return true;
        }
        if (mod(e) && !e.shiftKey && /^[1-9]$/.test(e.key) && Number(e.key) <= nav.length) {
            const to = nav[Number(e.key) - 1][0];
            void leave(() => navigate(to));
            return true;
        }
        if (!typing && (e.key === "?" || e.key === "F1")) {
            onHelp();
            return true;
        }
        return false;
    });
    return null;
}

function ShortcutHelp({onClose}: {onClose: () => void}) {
    const ref = useDialog<HTMLDivElement>(onClose);
    const entries = useNav().length;
    const rows: [string[], string][] = [
        [[key("S")], "Speichern (Rechnung, Posteingang, Einstellungen)"],
        [[key("Enter")], "Rechnung speichern und nächste erfassen"],
        [[key("N")], "Neue Rechnung"],
        [[key("F"), "/"], "Suchen in der Rechnungsliste"],
        [[key("1"), key(String(entries))], `Menüpunkte der Reihe nach (1 bis ${entries})`],
        [isMac ? ["⌫", "⌘["] : ["Rücktaste", "Alt+Pfeil links"], "Zurück (nicht beim Tippen in einem Feld)"],
        [isMac ? ["⌘]"] : ["Alt+Pfeil rechts"], "Vor"],
        [["Esc"], "Dialog schließen"],
        [["?"], "Diese Übersicht"],
    ];
    return (
        <div className="modal-back" onClick={e => e.target === e.currentTarget && onClose()}>
            <div ref={ref} className="modal narrow" role="dialog" aria-modal="true" aria-labelledby="shortcut-title" tabIndex={-1}>
                <h2 id="shortcut-title">Tastenkürzel</h2>
                <table className="shortcuts">
                    <tbody>
                    {rows.map(([keys, what]) => (
                        <tr key={what}>
                            <td>{keys.map((k, i) => <span key={k}>{i > 0 && <span className="muted small"> {keys[0].endsWith("1") ? "bis" : "oder"} </span>}<kbd>{k}</kbd></span>)}</td>
                            <td>{what}</td>
                        </tr>
                    ))}
                    </tbody>
                </table>
                <div className="actions"><button className="primary" onClick={onClose}>Schließen</button></div>
            </div>
        </div>
    );
}

/** A fresh form every time "Neue Rechnung" is opened, also from the form itself (Ctrl+N). */
function NewInvoiceForm() {
    const location = useLocation();
    return <InvoiceForm key={location.key}/>;
}
