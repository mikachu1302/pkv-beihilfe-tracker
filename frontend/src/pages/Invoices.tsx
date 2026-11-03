import {useEffect, useMemo, useRef, useState} from "react";
import {Link, useNavigate} from "react-router";
import {Desktop, Svc} from "../api";
import type {ClaimViewDTO, InvoiceRowDTO, MarkChangeDTO, PayerDTO, YearDTO} from "../api";
import {useApp} from "../App";
import {useConfirm, useToast} from "../components/feedback";
import {OutstandingCards} from "../components/Outstanding";
import {Empty, ErrorNotice, Loading, YearSelect} from "../components/ui";
import {patchAppearance, setSessionView, useAppearance, useInvoiceView, VIEW_LABELS} from "../lib/appearance";
import type {InvoiceView} from "../lib/appearance";
import {useDesign} from "../lib/design";
import {eur, num, plural, today} from "../lib/format";
import {useBackend} from "../lib/useBackend";
import {dialogOpen, isTyping, key, mod, useKeydown} from "../lib/shortcuts";
import {ClassicList} from "./invoices/ClassicList";
import {CompactView} from "./invoices/CompactView";
import {DetailView} from "./invoices/DetailView";
import {ColumnChooser, TableView} from "./invoices/TableView";
import type {TableOptions} from "./invoices/TableView";
import type {Pop, ViewProps} from "./invoices/shared";
import {exportExcel} from "./Overview";

/** The views of the new design, in the order of the switch. */
const VIEWS: InvoiceView[] = ["detail", "compact", "table", "classic"];

const DEFAULT_TABLE: TableOptions = {dense: false, sum: true, hidden: []};

/** Options of the table view (device configuration); changes are saved right away. */
function useTableOptions(): [TableOptions, (o: TableOptions) => void] {
    const toast = useToast();
    const a = useAppearance();
    const options = a ? {...a.table, hidden: a.table.hidden ?? []} : DEFAULT_TABLE;
    return [options, o => {
        patchAppearance({table: o});
        Svc.SetTableOptions(o).catch(toast.error);
    }];
}

type Filter = "all" | "open" | "submitted" | "clarify" | "deadline" | "unpaid" | "warnings";

const FILTERS: [Filter, string][] = [
    ["all", "Alle"],
    ["open", "Nicht eingereicht"],
    ["submitted", "Erstattung offen"],
    ["clarify", "Klärungsbedarf"],
    ["deadline", "Frist bald"],
    ["unpaid", "Nicht bezahlt"],
    ["warnings", "Mit Hinweisen"],
];

export function matchesFilter(r: InvoiceRowDTO, f: Filter): boolean {
    const cl = r.claims ?? [];
    switch (f) {
        case "open":
            return cl.some(c => c.status === "open" || c.status === "held");
        case "submitted":
            return cl.some(c => c.status === "submitted");
        case "clarify":
            return cl.some(c => c.clarify.done || c.suggestClarify);
        case "deadline":
            return cl.some(c => c.deadline !== "" && c.deadlineDays <= 90);
        case "unpaid":
            return !r.paid.done;
        case "warnings": // plausibility findings, e.g. typos taken over from the old spreadsheet
            return (r.warnings ?? []).length > 0;
        default:
            return true;
    }
}

export default function Invoices() {
    const {year, setYear, dataVersion} = useApp();
    const navigate = useNavigate();
    const design = useDesign();
    const view = useInvoiceView();
    const [tableOptions, setTableOptions] = useTableOptions();
    const toast = useToast();
    const confirm = useConfirm();
    const years = useBackend(() => Svc.Years(), [dataVersion]);
    const settings = useBackend(() => Svc.GetSettings(), [dataVersion]);
    const list = useBackend(() => Svc.ListInvoices(year), [year, dataVersion]);
    const [rows, setRows] = useState<InvoiceRowDTO[]>([]);
    const [filter, setFilter] = useState<Filter>("all");
    const [query, setQuery] = useState("");
    const [figures, setFigures] = useState(0); // bumped after own changes: the Ausstand cards reload
    const search = useRef<HTMLInputElement>(null);
    useKeydown(e => {
        if (dialogOpen() || !(mod(e) && e.key.toLowerCase() === "f" || e.key === "/" && !isTyping(e.target))) return false;
        search.current?.focus();
        search.current?.select();
        return true;
    });
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [pop, setPop] = useState<Pop | null>(null);

    useEffect(() => {
        setRows(list.data ?? []);
        setSelected(new Set());
    }, [list.data]);

    // Payer columns: payers that occur in this year's invoices, in settings order.
    const payers: PayerDTO[] = useMemo(() => {
        const used = new Set(rows.flatMap(r => (r.claims ?? []).map(c => c.payerId)));
        const all = settings.data?.payers ?? [];
        const known = all.filter(p => used.has(p.id));
        const unknown = [...used].filter(id => !all.some(p => p.id === id))
            .map(id => ({id, name: id, kind: "", claimDeadlineYears: 0, minClaimAmount: "", note: ""}));
        return [...known, ...unknown];
    }, [rows, settings.data]);

    const q = query.trim().toLowerCase();
    const visible = rows.filter(r => matchesFilter(r, filter) &&
        (!q || `${r.provider} ${r.reference} ${r.note}`.toLowerCase().includes(q)));

    async function change(ch: MarkChangeDTO) {
        try {
            const row = await Svc.SetMark(ch);
            setRows(rs => rs.map(r => (r.id === row.id ? row : r)));
            setFigures(n => n + 1);
            setPop(null);
        } catch (e) {
            toast.error(e);
        }
    }

    async function toggleRefunded(r: InvoiceRowDTO, c: ClaimViewDTO, on: boolean) {
        if (on) {
            setPop({rowId: r.id, payerId: c.payerId, kind: "refund"});
            return;
        }
        if (num(c.actual) !== 0 && !await confirm(`Erstattungen von ${c.payerName} (${eur(c.actual)}) entfernen?`, "Entfernen")) return;
        await change({invoiceId: r.id, payerId: c.payerId, mark: "refunded", on: false, date: "", note: "", amount: ""});
    }

    async function toggleClarify(r: InvoiceRowDTO, c: ClaimViewDTO) {
        if (!c.clarify.done) {
            setPop({rowId: r.id, payerId: c.payerId, kind: "clarify"});
            return;
        }
        await change({invoiceId: r.id, payerId: c.payerId, mark: "clarify", on: false, date: "", note: "", amount: ""});
    }

    const allSelected = visible.length > 0 && visible.every(r => selected.has(r.id));
    const toggleAll = () => setSelected(allSelected ? new Set() : new Set(visible.map(r => r.id)));
    const toggleOne = (id: string) => setSelected(s => {
        const n = new Set(s);
        if (n.has(id)) n.delete(id); else n.add(id);
        return n;
    });

    const viewProps: ViewProps = {
        rows: visible, payers, selected, toggleOne, allSelected, toggleAll, pop, setPop, change, toggleRefunded, toggleClarify,
        open: id => navigate(`/rechnung/${encodeURIComponent(id)}`),
    };

    const yearList = years.data ?? [year];
    return (
        <>
            <div className="page-head">
                <h1>Rechnungen</h1>
                <YearSelect years={yearList.includes(year) ? yearList : [year, ...yearList]} value={year}
                            onChange={setYear} allLabel="alle Jahre"/>
                <span className="spacer"/>
                <input ref={search} aria-label="Suchen" placeholder={`Suchen … (${key("F")})`} className="search" value={query}
                       onChange={e => setQuery(e.target.value)}/>
                <button onClick={() => exportExcel(toast, year)}>Excel</button>
                <button className="primary" onClick={() => navigate("/rechnung/neu")}>Neue Rechnung</button>
            </div>
            {design === "new" && (
                <div className="view-bar">
                    <ViewSwitch view={view}/>
                    {view !== "classic" && visible.length > 0 && (
                        <div className="view-tools">
                            {view === "table" && (
                                <>
                                    <span className="muted">Nur ansehen · Doppelklick öffnet die Rechnung</span>
                                    <ColumnChooser payers={payers.length} options={tableOptions} onChange={setTableOptions}/>
                                    <select aria-label="Zeilen" className="w-auto" value={tableOptions.dense ? "dense" : "normal"}
                                            onChange={e => setTableOptions({...tableOptions, dense: e.target.value === "dense"})}>
                                        <option value="normal">Zeilen: normal</option>
                                        <option value="dense">Zeilen: eng</option>
                                    </select>
                                </>
                            )}
                            <label className="inline">
                                <input type="checkbox" checked={allSelected} onChange={toggleAll}/>
                                Alle auswählen
                            </label>
                        </div>
                    )}
                </div>
            )}
            <OutstandingForYear year={year} version={figures}/>
            <div className="chips mb-3">
                {FILTERS.map(([f, label]) => {
                    const count = f === "warnings" ? rows.filter(r => matchesFilter(r, f)).length : 0;
                    return (
                        <button key={f} className={`chip ${filter === f ? "on" : ""}`} aria-pressed={filter === f}
                                onClick={() => setFilter(f)}>{label}{count > 0 && ` (${count})`}</button>
                    );
                })}
            </div>
            {list.error && <ErrorNotice error={list.error}/>}
            {selected.size > 0 && (
                <BulkBar ids={[...selected]} payers={payers} onDone={() => {
                    list.reload();
                    setFigures(n => n + 1);
                }}/>
            )}
            {list.loading && !list.data ? <Loading/> : visible.length === 0 ? (
                <Empty>
                    {rows.length === 0
                        ? <>Für {year === 0 ? "diesen Zeitraum" : year} sind noch keine Rechnungen erfasst. <Link to="/rechnung/neu">Neue Rechnung</Link> oder <Link to="/import">Excel importieren</Link>.</>
                        : "Keine Rechnung passt zum Filter."}
                </Empty>
            ) : view === "detail" ? <DetailView {...viewProps}/>
                : view === "compact" ? <CompactView {...viewProps}/>
                : view === "table" ? <TableView {...viewProps} options={tableOptions}/>
                    : <ClassicList {...viewProps}/>}
        </>
    );
}

/** Detail, Kompakt, Tabelle or the classic list (new design only). */
function ViewSwitch({view}: {view: InvoiceView}) {
    return (
        <div className="segmented" role="group" aria-label="Ansicht">
            {VIEWS.map(v => (
                <button key={v} type="button" aria-pressed={view === v} onClick={() => setSessionView(v)}>{VIEW_LABELS[v]}</button>
            ))}
        </div>
    );
}

function BulkBar({ids, payers, onDone}: {ids: string[]; payers: PayerDTO[]; onDone: () => void}) {
    const toast = useToast();
    const [payerId, setPayerId] = useState(payers[0]?.id ?? "");
    const [d, setD] = useState(today());
    const [channel, setChannel] = useState("");

    async function bundle() {
        try {
            const payer = payers.find(p => p.id === payerId);
            const path = await Desktop.ChooseSaveFile("PDF für die Einreichung speichern", `Einreichung ${payer?.name ?? ""} ${d}.pdf`);
            if (!path) return;
            const res = await Svc.ExportSubmission(ids, payerId, path);
            const missing = res.missing ?? [];
            toast.show(`PDF mit ${plural(res.invoices, "Rechnung", "Rechnungen")} erstellt` +
                (missing.length ? ` – ohne Beleg: ${missing.join(", ")}` : ""), missing.length > 0);
            await Desktop.OpenPath(res.path);
        } catch (e) {
            toast.error(e);
        }
    }

    async function submit() {
        try {
            const res = await Svc.BulkSubmit({invoiceIds: ids, payerId, date: d, channel, note: ""});
            const skipped = res.skipped ?? [];
            toast.show(`${plural(res.marked, "Rechnung", "Rechnungen")} als eingereicht markiert` +
                (skipped.length ? ` – übersprungen: ${skipped.join("; ")}` : ""), skipped.length > 0 && res.marked === 0);
            onDone();
        } catch (e) {
            toast.error(e);
        }
    }

    return (
        <div className="bulkbar" role="region" aria-label="Auswahl">
            <strong>{plural(ids.length, "Rechnung", "Rechnungen")} ausgewählt</strong>
            <span>bei</span>
            <select aria-label="Kostenträger" value={payerId} onChange={e => setPayerId(e.target.value)}>
                {payers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
            </select>
            <span>eingereicht am</span>
            <input type="date" aria-label="Einreichungsdatum" value={d} onChange={e => setD(e.target.value)}/>
            <input aria-label="Weg" placeholder="Weg (App, Post …)" value={channel} onChange={e => setChannel(e.target.value)}/>
            <button className="primary" disabled={!payerId} onClick={submit}>Als eingereicht markieren</button>
            <button disabled={!payerId} onClick={bundle} title="Deckblatt und alle Belege in einer PDF – z. B. zum Hochladen in die Beihilfe-App">PDF-Paket</button>
        </div>
    );
}

/** Outstanding amounts for the chosen year or all years, above the list. */
function OutstandingForYear({year, version}: {year: number; version: number}) {
    const {dataVersion} = useApp();
    const figures = useBackend<YearDTO>(() => (year === 0 ? Svc.Overview().then(o => o.total) : Svc.Dashboard(year)), [year, dataVersion, version]);
    return <OutstandingCards payers={figures.data?.payers ?? []} period={year === 0 ? "alle Jahre" : String(year)}/>;
}
