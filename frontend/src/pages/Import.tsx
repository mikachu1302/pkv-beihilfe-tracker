import {useEffect, useState} from "react";
import {Desktop, Svc} from "../api";
import type {ImportPreviewDTO, ImportRowDTO} from "../api";
import {useToast} from "../components/feedback";
import {Badge, ErrorNotice, Field, Loading, Notice} from "../components/ui";
import {date, errMsg, eur, plural} from "../lib/format";
import {useBackend} from "../lib/useBackend";

/** Import of the old spreadsheet with preview. */
export default function Import() {
    const toast = useToast();
    const settings = useBackend(() => Svc.GetSettings(), []);
    const payers = settings.data?.payers ?? [];
    const beihilfen = payers.filter(p => p.kind === "beihilfe");
    const pkvs = payers.filter(p => p.kind === "pkv");
    const [bh, setBh] = useState("");
    const [pkv, setPkv] = useState("");
    const [preview, setPreview] = useState<ImportPreviewDTO | null>(null);
    const [include, setInclude] = useState<Set<string>>(new Set());
    const [showUnchanged, setShowUnchanged] = useState(false);
    const [error, setError] = useState("");
    const [busy, setBusy] = useState(false);

    // Preselect the first Beihilfe and PKV once the settings are there.
    useEffect(() => {
        const all = settings.data?.payers ?? [];
        setBh(b => b || (all.find(x => x.kind === "beihilfe")?.id ?? ""));
        setPkv(p => p || (all.find(x => x.kind === "pkv")?.id ?? ""));
    }, [settings.data]);

    async function choose() {
        setError("");
        try {
            const path = await Desktop.ChooseFile("Excel-Datei wählen", "Excel", "*.xlsx;*.xlsm");
            if (!path) return;
            setBusy(true);
            const pv = await Svc.PreviewSpreadsheet(path, bh, pkv);
            setPreview(pv);
            // New rows and certain changes are taken over; matches by similarity
            // only and deletions wait for the user.
            setInclude(new Set((pv.rows ?? []).filter(r => r.state === "new" || r.state === "changed" && !r.uncertain).map(r => r.key)));
        } catch (e) {
            setError(errMsg(e));
        } finally {
            setBusy(false);
        }
    }

    async function commit() {
        if (!preview) return;
        setBusy(true);
        try {
            const res = await Svc.CommitSpreadsheet(preview.token, [...include]);
            const done = [
                res.invoices && `${plural(res.invoices, "Rechnung", "Rechnungen")} neu`,
                res.updated && `${res.updated} aktualisiert`,
                res.deleted && `${res.deleted} gelöscht`,
                res.allowances && `${plural(res.allowances, "Leistung", "Leistungen")} ohne Rechnung`,
            ].filter(Boolean);
            toast.show(done.length ? `Übernommen: ${done.join(", ")}` : "Nichts zu übernehmen");
            setPreview(null);
        } catch (e) {
            setError(errMsg(e));
        } finally {
            setBusy(false);
        }
    }

    const toggle = (key: string) => setInclude(s => {
        const n = new Set(s);
        if (n.has(key)) n.delete(key); else n.add(key);
        return n;
    });

    if (settings.error) return <ErrorNotice error={settings.error}/>;
    if (!settings.data) return <Loading/>;
    const allRows = preview?.rows ?? [];
    const rows = showUnchanged ? allRows : allRows.filter(r => r.state !== "unchanged");
    const selectable = rows.filter(r => r.state !== "unchanged");
    const removed = preview?.removed ?? [];
    const chosen = (state: string) => allRows.filter(r => r.state === state && include.has(r.key)).length;
    const chosenRemoved = removed.filter(r => include.has(r.key)).length;
    const total = chosen("new") + chosen("changed") + chosenRemoved;
    // The import reads every sheet with a table: name the sheet when there is more than one.
    const sheets = new Set(allRows.map(r => r.sheet)).size > 1;
    const where = (r: ImportRowDTO) => (sheets ? `${r.sheet}, Zeile ${r.row}` : `Zeile ${r.row}`);
    return (
        <>
            <div className="page-head"><h1>Excel importieren</h1></div>
            <div className="panel">
                <p>
                    Liest eine Tabelle im Aufbau der bisherigen Excel-Datei („Abrechnung Krankenkasse“): eine Zeile pro
                    Rechnung mit Rechnungsdatum, Behandler, Gesamtbetrag sowie Einreichungsdatum und erstattetem Betrag
                    für Beihilfe und Krankenkasse. Die Spalten werden an ihren Überschriften erkannt. Vor dem Speichern
                    gibt es eine Vorschau.
                </p>
                <div className="grid">
                    <Field label="Spalten „Beihilfe“ gehören zu" htmlFor="imp-bh">
                        <select id="imp-bh" value={bh} onChange={e => setBh(e.target.value)}>
                            <option value="">– nicht übernehmen –</option>
                            {beihilfen.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                        </select>
                    </Field>
                    <Field label="Spalten „Krankenkasse“ gehören zu" htmlFor="imp-pkv">
                        <select id="imp-pkv" value={pkv} onChange={e => setPkv(e.target.value)}>
                            {pkvs.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                        </select>
                    </Field>
                </div>
                <div className="actions">
                    <button className="primary" disabled={busy || !pkv} onClick={choose}>Excel-Datei wählen …</button>
                </div>
            </div>
            {error && <ErrorNotice error={error}/>}
            {preview && (
                <>
                    <div className="panel">
                        <strong>{preview.file}</strong> · {sheets ? "Blätter" : "Blatt"} „{preview.sheet}“ · {plural(allRows.length, "Rechnung", "Rechnungen")}
                        {" "}({(preview.years ?? []).map(y => `${y.year || "ohne Datum"}: ${y.count}`).join(", ")})
                        {preview.allowances > 0 && <> · {plural(preview.allowances, "Leistung", "Leistungen")} ohne Rechnung</>}
                        <div className="chips" style={{marginTop: 8}}>
                            <Badge kind="good">{preview.new} neu</Badge>
                            <Badge kind="warn">{preview.changed} geändert</Badge>
                            <Badge>{preview.unchanged} unverändert</Badge>
                            {removed.length > 0 && <Badge kind="bad">{removed.length} nicht mehr in der Excel</Badge>}
                        </div>
                        {preview.changed > 0 && <p className="muted small" style={{marginBottom: 0}}>
                            Geänderte Zeilen aktualisieren die vorhandene Rechnung – nur die aufgeführten Felder. Erstattungen aus Bescheiden,
                            Belege, Notizen aus der App und Klärungsbedarf bleiben erhalten.
                        </p>}
                        {preview.warnings > 0 && <Notice kind="warn">{plural(preview.warnings, "Hinweis", "Hinweise")} zu einzelnen Zeilen – bitte prüfen (Tippfehler bei Daten, Erstattungen über dem Rechnungsbetrag). Korrigieren lässt sich alles auch nach dem Import.</Notice>}
                    </div>
                    {preview.unchanged > 0 && (
                        <label className="inline" style={{margin: "0 0 8px"}}>
                            <input type="checkbox" checked={showUnchanged} onChange={e => setShowUnchanged(e.target.checked)}/>
                            Unveränderte Zeilen zeigen ({preview.unchanged})
                        </label>
                    )}
                    {rows.length > 0 && (
                        <div className="table-wrap">
                            <table>
                                <thead>
                                <tr>
                                    <th className="check-col">
                                        <input type="checkbox" aria-label="Alle auswählen"
                                               checked={selectable.length > 0 && selectable.every(r => include.has(r.key))}
                                               onChange={() => setInclude(s => {
                                                   const all = selectable.every(r => s.has(r.key));
                                                   const n = new Set(s);
                                                   selectable.forEach(r => (all ? n.delete(r.key) : n.add(r.key)));
                                                   return n;
                                               })}/>
                                    </th>
                                    <th>{sheets ? "Blatt / Zeile" : "Zeile"}</th><th>Rechnungsdatum</th><th>Behandler</th><th className="num">Betrag</th>
                                    <th>Abgleich</th><th>Status in der Excel</th><th>Hinweise</th>
                                </tr>
                                </thead>
                                <tbody>
                                {rows.map(r => (
                                    <tr key={r.key} className={r.state === "unchanged" ? "muted" : ""}>
                                        <td>
                                            <input type="checkbox" aria-label={`${where(r)} übernehmen`} disabled={r.state === "unchanged"}
                                                   checked={include.has(r.key)} onChange={() => toggle(r.key)}/>
                                        </td>
                                        <td className="muted">{sheets ? `${r.sheet} · ${r.row}` : r.row}</td>
                                        <td>{date(r.invoiceDate)}</td>
                                        <td>{r.provider}{r.note && <div className="sub">{r.note}</div>}</td>
                                        <td className="num">{eur(r.amount)}</td>
                                        <td className="small">
                                            <StateBadge state={r.state}/>
                                            {r.uncertain && <div className="neg">Zuordnung bitte prüfen</div>}
                                            {(r.changes ?? []).map(c => (
                                                <div key={c.label}>
                                                    {c.label}: {c.from} → <strong>{c.to}</strong>
                                                    {c.conflict && <span className="neg" title="Der Wert wurde auch in der App geändert und wird überschrieben"> (in der App geändert)</span>}
                                                </div>
                                            ))}
                                        </td>
                                        <td className="small">{(r.status ?? []).map(s => <div key={s}>{s}</div>)}</td>
                                        <td className="small">{(r.warnings ?? []).map(w => <div key={w}>{w}</div>)}</td>
                                    </tr>
                                ))}
                                </tbody>
                            </table>
                        </div>
                    )}
                    {removed.length > 0 && (
                        <div className="panel" style={{marginTop: 12}}>
                            <strong>Nicht mehr in der Excel</strong>
                            <p className="muted small">Früher aus dieser Datei importiert, dort aber nicht mehr vorhanden. Nur ausgewählte werden gelöscht.</p>
                            {removed.map(r => (
                                <label key={r.key} className="inline" style={{display: "flex"}}>
                                    <input type="checkbox" checked={include.has(r.key)} onChange={() => toggle(r.key)}/>
                                    löschen: {r.label} <span className="muted small">({sheets ? `${r.sheet}, ` : ""}früher Zeile {r.row})</span>
                                </label>
                            ))}
                        </div>
                    )}
                    <div className="actions">
                        <button className="primary" disabled={busy || total === 0} onClick={commit}>
                            Übernehmen{total > 0 && ` (${[chosen("new") && `${chosen("new")} neu`, chosen("changed") && `${chosen("changed")} geändert`,
                                chosenRemoved && `${chosenRemoved} löschen`].filter(Boolean).join(", ")})`}
                        </button>
                        <button onClick={() => setPreview(null)}>Verwerfen</button>
                    </div>
                </>
            )}
        </>
    );
}

function StateBadge({state}: {state: string}) {
    switch (state) {
        case "new":
            return <Badge kind="good">neu</Badge>;
        case "changed":
            return <Badge kind="warn">geändert</Badge>;
        default:
            return <Badge>unverändert</Badge>;
    }
}
