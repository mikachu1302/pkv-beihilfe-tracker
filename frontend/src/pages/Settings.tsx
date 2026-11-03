import {useEffect, useRef, useState} from "react";
import {Desktop, Svc} from "../api";
import type {BackupStatusDTO, CoverageDTO, PayerDTO, ProblemDTO, SettingsDTO, YearSettingDTO} from "../api";
import {useApp, useSetDesign, useSetTheme} from "../App";
import {useToast} from "../components/feedback";
import {ErrorNotice, Field, Loading, Notice} from "../components/ui";
import {errMsg, toInput} from "../lib/format";
import {key, useSaveShortcut, useUnsavedChanges} from "../lib/shortcuts";
import {useBackend} from "../lib/useBackend";
import {patchAppearance, setSessionView, useAppearance, VIEW_LABELS, VIEW_ORDER} from "../lib/appearance";
import type {InvoiceView} from "../lib/appearance";
import {DESIGN_LABELS, DESIGN_ORDER, useDesign} from "../lib/design";
import type {Design} from "../lib/design";
import {THEME_LABELS, THEME_ORDER, useTheme} from "../lib/theme";
import type {Theme} from "../lib/theme";
import {COLUMN_KINDS, useTableOptions} from "./invoices/TableView";

/** Amounts in the form use the German format. */
function forForm(s: SettingsDTO): SettingsDTO {
    return {
        ...s,
        payers: (s.payers ?? []).map(p => ({...p, minClaimAmount: toInput(p.minClaimAmount)})),
        coverage: (s.coverage ?? []).map(c => ({...c, shares: (c.shares ?? []).map(sh => ({...sh, rate: sh.rate.replace(".", ",")}))})),
        years: (s.years ?? []).map(y => ({...y, deductible: toInput(y.deductible), annualDeduction: toInput(y.annualDeduction),
            expectedPremiumRefund: toInput(y.expectedPremiumRefund)})),
        clarifyTolerance: toInput(s.clarifyTolerance),
    };
}

function newPayerID(kind: string, payers: PayerDTO[]): string {
    for (let i = 2; ; i++) {
        const id = `${kind}-${i}`;
        if (!payers.some(p => p.id === id)) return id;
    }
}

export default function Settings() {
    const {status, reloadStatus, dataVersion} = useApp();
    const toast = useToast();
    // Loaded again when another device changed the data folder or another folder was chosen.
    const loaded = useBackend(() => Svc.GetSettings(), [dataVersion, status.dataDir]);
    // The settings the form is based on (as loaded or last saved), and newer
    // ones that arrived while the form had unsaved edits.
    const [base, setBase] = useState<SettingsDTO | null>(null);
    const [newer, setNewer] = useState<SettingsDTO | null>(null);
    const [seen, setSeen] = useState<SettingsDTO>();
    const [s, setS] = useState<SettingsDTO | null>(null);
    const [error, setError] = useState("");
    const [warnings, setWarnings] = useState<ProblemDTO[]>([]);
    const savingRef = useRef(false);

    const edited = s !== null && base !== null && JSON.stringify(s) !== JSON.stringify(forForm(base));
    const apply = (d: SettingsDTO) => {
        setBase(d);
        setS(forForm(d));
        setNewer(null);
    };
    // Fresh settings from the backend replace the form unless it has unsaved
    // edits – those are kept and the user decides (hint below).
    if (loaded.data && loaded.data !== seen) {
        setSeen(loaded.data);
        if (!edited) apply(loaded.data);
        else if (JSON.stringify(loaded.data) !== JSON.stringify(base)) setNewer(loaded.data);
    }

    useUnsavedChanges(edited);
    useSaveShortcut(() => void save(), s !== null);

    if (loaded.error) return <ErrorNotice error={loaded.error}/>;
    if (!s) return <Loading/>;
    const payers = s.payers ?? [];
    const coverage = s.coverage ?? [];
    const years = s.years ?? [];

    const setPayer = (i: number, patch: Partial<PayerDTO>) => setS({...s, payers: payers.map((p, j) => (j === i ? {...p, ...patch} : p))});
    const addPayer = (kind: "beihilfe" | "pkv") => setS({...s, payers: [...payers, {id: newPayerID(kind, payers), kind,
        name: "", claimDeadlineYears: 0, minClaimAmount: "", note: ""}]});
    const removePayer = (i: number) => setS({...s, payers: payers.filter((_, j) => j !== i)});

    const rateOf = (c: CoverageDTO, payerId: string) => (c.shares ?? []).find(sh => sh.payerId === payerId)?.rate ?? "";
    const setRate = (ci: number, payerId: string, rate: string) => setS({
        ...s, coverage: coverage.map((c, j) => {
            if (j !== ci) return c;
            const others = (c.shares ?? []).filter(sh => sh.payerId !== payerId);
            return {...c, shares: rate.trim() ? [...others, {payerId, rate}] : others};
        }),
    });
    const setFrom = (ci: number, from: string) => setS({...s, coverage: coverage.map((c, j) => (j === ci ? {...c, from} : c))});
    const addPeriod = () => {
        const last = coverage[coverage.length - 1];
        setS({...s, coverage: [...coverage, {from: "", shares: last ? [...(last.shares ?? [])] : []}]});
    };
    const removePeriod = (ci: number) => setS({...s, coverage: coverage.filter((_, j) => j !== ci)});

    const setYearRow = (i: number, patch: Partial<YearSettingDTO>) => setS({...s, years: years.map((y, j) => (j === i ? {...y, ...patch} : y))});
    const addYearRow = () => setS({...s, years: [...years, {year: new Date().getFullYear(), payerId: payers[0]?.id ?? "",
        deductible: "", annualDeduction: "", expectedPremiumRefund: ""}]});
    const removeYearRow = (i: number) => setS({...s, years: years.filter((_, j) => j !== i)});

    async function save() {
        if (!s || savingRef.current) return;
        savingRef.current = true;
        setError("");
        try {
            // The backend merges: values changed elsewhere since base was loaded are kept
            // unless this form changed the same value.
            const res = await Svc.SaveSettings(s, base);
            apply(res.settings);
            setWarnings(res.problems ?? []);
            toast.show("Einstellungen gespeichert");
            reloadStatus();
        } catch (e) {
            setError(errMsg(e));
        } finally {
            savingRef.current = false;
        }
    }

    return (
        <>
            <div className="page-head"><h1>Einstellungen</h1></div>
            <DataFolder dir={status.dataDir} onChanged={() => {
                reloadStatus();
                loaded.reload();
            }}/>
            <Appearance/>
            <Backups onChanged={reloadStatus}/>
            {newer && (
                <Notice kind="warn">
                    <div className="row">
                        <span>
                            Die Einstellungen wurden inzwischen geändert (anderes Gerät oder anderer Datenordner) – neu laden?
                            „Speichern“ übernimmt nur die hier geänderten Werte, „Neu laden“ verwirft sie.
                        </span>
                        <button onClick={() => apply(newer)}>Neu laden</button>
                    </div>
                </Notice>
            )}
            {error && <ErrorNotice error={error}/>}
            {warnings.length > 0 && <Notice kind="warn"><ul>{warnings.map(w => <li key={w.text}>{w.text}</li>)}</ul></Notice>}

            <section className="panel">
                <h2>Kostenträger</h2>
                <p className="muted small">Beihilfestellen und private Krankenversicherungen. Frist und Mindestbetrag unterscheiden sich je nach Land – Bund (BBhV): 3 Jahre nach Rechnungsdatum, mehr als 200 € pro Antrag.</p>
                <table>
                    <thead><tr><th>Name</th><th>Art</th><th>Antragsfrist (Jahre)</th><th>Mindestbetrag (€)</th><th>Notiz</th><th/></tr></thead>
                    <tbody>
                    {payers.map((p, i) => (
                        <tr key={p.id}>
                            <td><input aria-label="Name" value={p.name} onChange={e => setPayer(i, {name: e.target.value})}/></td>
                            <td>
                                <select aria-label="Art" value={p.kind} onChange={e => setPayer(i, {kind: e.target.value})}>
                                    <option value="beihilfe">Beihilfe</option>
                                    <option value="pkv">PKV</option>
                                </select>
                            </td>
                            <td><input aria-label="Antragsfrist" type="number" min={0} max={10} value={p.claimDeadlineYears || ""}
                                       placeholder="keine" onChange={e => setPayer(i, {claimDeadlineYears: Number(e.target.value) || 0})}/></td>
                            <td><input aria-label="Mindestbetrag" value={p.minClaimAmount} placeholder="keiner" onChange={e => setPayer(i, {minClaimAmount: e.target.value})}/></td>
                            <td><input aria-label="Notiz" value={p.note} onChange={e => setPayer(i, {note: e.target.value})}/></td>
                            <td>
                                <span className="row nowrap">
                                    {p.kind === "beihilfe" && <button className="link" onClick={() => setPayer(i, {claimDeadlineYears: 3, minClaimAmount: "200,00"})}>BBhV</button>}
                                    <button className="link" aria-label={`${p.name || "Kostenträger"} entfernen`} onClick={() => removePayer(i)}>✕</button>
                                </span>
                            </td>
                        </tr>
                    ))}
                    </tbody>
                </table>
                <div className="actions">
                    <button onClick={() => addPayer("beihilfe")}>+ Beihilfestelle</button>
                    <button onClick={() => addPayer("pkv")}>+ PKV</button>
                </div>
            </section>

            <section className="panel">
                <h2>Versicherungsschutz</h2>
                <p className="muted small">Welcher Kostenträger ab wann welchen Anteil trägt. Es gilt der Zeitraum, in dem die Behandlung stattfand (z. B. 50/50, nach der Pensionierung 70/30). Leeres Feld = kein Anteil.</p>
                <table>
                    <thead><tr><th>Gültig ab</th>{payers.map(p => <th key={p.id}>{p.name || "(neu)"} (%)</th>)}<th/></tr></thead>
                    <tbody>
                    {coverage.map((c, ci) => (
                        <tr key={ci}>
                            <td><input type="date" aria-label="Gültig ab" value={c.from} onChange={e => setFrom(ci, e.target.value)}/></td>
                            {payers.map(p => (
                                <td key={p.id}><input aria-label={`Anteil ${p.name}`} value={rateOf(c, p.id)} onChange={e => setRate(ci, p.id, e.target.value)}/></td>
                            ))}
                            <td><button className="link" aria-label="Zeitraum entfernen" onClick={() => removePeriod(ci)}>✕</button></td>
                        </tr>
                    ))}
                    </tbody>
                </table>
                <div className="actions"><button onClick={addPeriod}>+ Zeitraum</button></div>
            </section>

            <section className="panel">
                <h2>Werte pro Jahr</h2>
                <p className="muted small">Lassen sich auch direkt im Dashboard eintragen.</p>
                {years.length > 0 && (
                    <table>
                        <thead><tr><th>Jahr</th><th>Kostenträger</th><th>Selbstbehalt (€)</th><th>Kostendämpfungspauschale (€)</th><th>Erwartete Beitragsrückerstattung (€)</th><th/></tr></thead>
                        <tbody>
                        {years.map((y, i) => (
                            <tr key={i}>
                                <td><input aria-label="Jahr" type="number" value={y.year} onChange={e => setYearRow(i, {year: Number(e.target.value)})}/></td>
                                <td>
                                    <select aria-label="Kostenträger" value={y.payerId} onChange={e => setYearRow(i, {payerId: e.target.value})}>
                                        {payers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                                    </select>
                                </td>
                                <td><input aria-label="Selbstbehalt" value={y.deductible} onChange={e => setYearRow(i, {deductible: e.target.value})}/></td>
                                <td><input aria-label="Kostendämpfungspauschale" value={y.annualDeduction} onChange={e => setYearRow(i, {annualDeduction: e.target.value})}/></td>
                                <td><input aria-label="Erwartete Beitragsrückerstattung" value={y.expectedPremiumRefund} onChange={e => setYearRow(i, {expectedPremiumRefund: e.target.value})}/></td>
                                <td><button className="link" aria-label="Zeile entfernen" onClick={() => removeYearRow(i)}>✕</button></td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                )}
                <div className="actions"><button onClick={addYearRow}>+ Jahr</button></div>
            </section>

            <section className="panel">
                <h2>Sonstiges</h2>
                <div className="grid">
                    <Field label="Toleranz für Klärungsbedarf (€)" htmlFor="tol" hint="Weicht IST um mehr vom SOLL ab, schlägt die App Klärungsbedarf vor">
                        <input id="tol" value={s.clarifyTolerance} onChange={e => setS({...s, clarifyTolerance: e.target.value})}/>
                    </Field>
                </div>
            </section>

            <div className="actions">
                <button className="primary" onClick={save} title={`Speichern (${key("S")})`}>Speichern</button>
                <button onClick={() => {
                    const d = newer ?? base;
                    if (d) apply(d);
                }}>Zurücksetzen</button>
            </div>
        </>
    );
}

/** Darstellung: settings of this device only (not synced with the data folder). */
function Appearance() {
    const theme = useTheme();
    const setTheme = useSetTheme();
    const design = useDesign();
    const setDesign = useSetDesign();
    const appearance = useAppearance();
    const setView = useSetView();
    const [table, setTable] = useTableOptions();
    const view = (appearance?.view ?? "detail") as InvoiceView;
    const hiddenCount = COLUMN_KINDS.filter(k => table.hidden.includes(k)).length;
    return (
        <section className="panel" aria-label="Darstellung">
            <h2>Darstellung</h2>
            <p className="muted small">Gilt nur für dieses Gerät; der Datenordner bleibt unberührt.</p>
            <div className="appearance">
                <fieldset className="choice-cards">
                    <legend>Design</legend>
                    {DESIGN_ORDER.map(d => (
                        <label key={d} className={`choice-card ${design === d ? "on" : ""}`}>
                            <input type="radio" name="design" checked={design === d} onChange={() => setDesign(d)}
                                   aria-labelledby={`design-${d}`} aria-describedby={`design-${d}-hint`}/>
                            <span>
                                <span className="choice-title" id={`design-${d}`}>{DESIGN_LABELS[d]}{d === "new" && " (Standard)"}</span>
                                <span className="choice-hint" id={`design-${d}-hint`}>{DESIGN_HINTS[d]}</span>
                            </span>
                        </label>
                    ))}
                </fieldset>
                {design === "new" && (
                    <fieldset className="choice-cards">
                        <legend>Standardansicht der Rechnungen</legend>
                        {VIEW_ORDER.map(v => (
                            <label key={v} className={`choice-card ${view === v ? "on" : ""}`}>
                                <input type="radio" name="view" checked={view === v} onChange={() => setView(v)}
                                       aria-labelledby={`view-${v}`} aria-describedby={`view-${v}-hint`}/>
                                <span>
                                    <span className="choice-title" id={`view-${v}`}>{VIEW_LABELS[v]}{v === "detail" && " (Standard)"}</span>
                                    <span className="choice-hint" id={`view-${v}-hint`}>{VIEW_HINTS[v]}</span>
                                </span>
                            </label>
                        ))}
                    </fieldset>
                )}
                <div className="appearance-side">
                    <fieldset className="choice stacked">
                        <legend>Farbschema</legend>
                        {THEME_ORDER.map((t: Theme) => (
                            <label key={t} className="inline">
                                <input type="radio" name="theme" checked={theme === t} onChange={() => setTheme(t)}/>
                                {THEME_LABELS[t]}
                            </label>
                        ))}
                    </fieldset>
                    {design === "new" && (
                        <fieldset className="choice stacked">
                            <legend>Tabellenansicht</legend>
                            <label className="inline">
                                <input type="checkbox" checked={table.sum} onChange={e => setTable({...table, sum: e.target.checked})}/>
                                Summenzeile
                            </label>
                            <label className="inline">
                                <input type="checkbox" checked={table.dense} onChange={e => setTable({...table, dense: e.target.checked})}/>
                                enge Zeilen
                            </label>
                            <span className="muted small">
                                {hiddenCount === 0 ? "Alle Spalten sichtbar" : `${hiddenCount} Spaltenart${hiddenCount === 1 ? "" : "en"} ausgeblendet`}
                                {" – "}auswählen in der Tabelle über „Spalten“.
                                {hiddenCount > 0 && <> <button className="link" onClick={() => setTable({...table, hidden: []})}>Alle zeigen</button></>}
                            </span>
                        </fieldset>
                    )}
                </div>
            </div>
            <p className="muted small mt-3">
                „Automatisch“ richtet sich nach der Einstellung von Windows, macOS oder Linux; schnell umschalten lässt es sich
                auch unten im Menü mit dem Schalter „Dunkel“.
                {design === "new" && " Die Ansicht lässt sich auch direkt in der Rechnungsliste wechseln – die Auswahl dort gilt bis zum nächsten Start."}
            </p>
        </section>
    );
}

const DESIGN_HINTS: Record<Design, string> = {
    new: "Ruhige Flächen, eine Akzentfarbe, Beträge in Festbreitenschrift; Rechnungen als Detail, Kompakt oder Tabelle",
    classic: "Das Aussehen bis Version 0.3 mit der bisherigen Rechnungsliste",
};

const VIEW_HINTS: Record<InvoiceView, string> = {
    detail: "Jede Rechnung als Karte mit SOLL, IST und Daten je Kostenträger",
    compact: "Eine Zeile pro Rechnung, Status als Punkte, Details zum Aufklappen",
    table: "Alle Felder als Spalten wie in Excel, sortierbar, nur zum Ansehen",
    classic: "Die bisherige Liste mit Häkchen in der Tabelle",
};

/** Sets the default view of the invoice list; a quick choice made in the list gives way to it. */
function useSetView(): (v: InvoiceView) => void {
    const toast = useToast();
    return v => {
        patchAppearance({view: v});
        setSessionView(null);
        Svc.SetView(v).catch(toast.error);
    };
}

export function Backups({onChanged}: {onChanged: () => void}) {
    const toast = useToast();
    const loaded = useBackend(() => Svc.BackupStatus(), []);
    const [st, setSt] = useState<BackupStatusDTO | null>(null);
    const [busy, setBusy] = useState(false);
    useEffect(() => {
        if (loaded.data) setSt(loaded.data);
    }, [loaded.data]);

    async function run(action: () => Promise<BackupStatusDTO>, done: string) {
        setBusy(true);
        try {
            setSt(await action());
            toast.show(done);
            onChanged();
        } catch (e) {
            toast.error(e);
            loaded.reload();
        } finally {
            setBusy(false);
        }
    }

    async function choose() {
        const d = await Desktop.ChooseFolder("Sicherungsordner wählen").catch(toast.error);
        if (d) await run(() => Svc.SetBackupDir(d), "Sicherungsordner gesetzt, erste Sicherung erstellt");
    }

    const mb = (bytes: number) => `${(bytes / 1024 / 1024).toLocaleString("de-DE", {maximumFractionDigits: 1})} MB`;
    return (
        <section className="panel" aria-label="Sicherung">
            <h2>Sicherung</h2>
            <p className="muted small">
                Einmal täglich beim Start legt die App eine Zip-Kopie des Datenordners in einem zweiten Ordner ab
                (z. B. USB-Stick, NAS oder ein anderer Cloud-Ordner); die letzten 20 werden behalten. Wichtig, wenn der
                Datenordner nicht in einer Cloud liegt. Zum Wiederherstellen eine Sicherung in einen leeren Ordner
                entpacken und diesen als Datenordner wählen.
            </p>
            {st?.error && <Notice kind="bad">{st.error}</Notice>}
            {st && (st.dir ? (
                <div className="row">
                    <code className="mono grow break">{st.dir}</code>
                    <span className="muted small">
                        {st.last ? `Letzte Sicherung ${st.last.split(" ")[0].split("-").reverse().join(".")} ${st.last.split(" ")[1]} (${mb(st.lastSize)}), ${st.count} vorhanden` : "Noch keine Sicherung"}
                    </span>
                    <button disabled={busy} onClick={() => run(() => Svc.BackupNow(), "Sicherung erstellt")}>Jetzt sichern</button>
                    <button onClick={() => Desktop.OpenPath(st.dir).catch(toast.error)}>Öffnen</button>
                    <button disabled={busy} onClick={choose}>Ändern …</button>
                    <button className="link" disabled={busy} onClick={() => run(() => Svc.SetBackupDir(""), "Automatische Sicherung ausgeschaltet")}>Ausschalten</button>
                </div>
            ) : (
                <div className="row">
                    <span className="muted">Keine Sicherung eingerichtet.</span>
                    <button className="primary" disabled={busy} onClick={choose}>Sicherungsordner wählen …</button>
                </div>
            ))}
        </section>
    );
}

function DataFolder({dir, onChanged}: {dir: string; onChanged: () => void}) {
    const toast = useToast();

    async function change() {
        try {
            const d = await Desktop.ChooseFolder("Datenordner wählen");
            if (!d) return;
            await Svc.SetDataDir(d);
            toast.show("Datenordner gewechselt");
            onChanged();
        } catch (e) {
            toast.error(e);
        }
    }

    async function reload() {
        try {
            await Svc.Reload();
            toast.show("Datenordner neu eingelesen");
            onChanged();
        } catch (e) {
            toast.error(e);
        }
    }

    return (
        <section className="panel">
            <h2>Datenordner</h2>
            <div className="row">
                <code className="mono grow break">{dir}</code>
                <button onClick={() => Desktop.OpenPath(dir).catch(toast.error)}>Öffnen</button>
                <button onClick={reload}>Neu einlesen</button>
                <button onClick={change}>Wechseln …</button>
            </div>
            <p className="muted small mt-2">
                Der Ordner kann mit jedem Sync-Programm geteilt werden. Am besten auf „immer auf diesem Gerät behalten“ stellen.
                Konfliktkopien werden beim Einlesen automatisch zusammengeführt.
            </p>
        </section>
    );
}
