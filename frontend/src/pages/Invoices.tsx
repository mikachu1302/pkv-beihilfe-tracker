import {useEffect, useMemo, useRef, useState} from "react";
import {Link, useNavigate} from "react-router";
import {Desktop, Svc} from "../api";
import type {ClaimViewDTO, InvoiceRowDTO, MarkChangeDTO, PayerDTO, YearDTO} from "../api";
import {useApp} from "../App";
import {useConfirm, useToast} from "../components/feedback";
import {MarkBox} from "../components/MarkBox";
import {Badge, Empty, ErrorNotice, Loading, YearSelect} from "../components/ui";
import {CATEGORY_LABELS, date, eur, num, plural, toInput, today} from "../lib/format";
import {useBackend} from "../lib/useBackend";
import {useDialog} from "../lib/useDialog";
import {dialogOpen, isTyping, key, mod, useKeydown} from "../lib/shortcuts";
import {exportExcel} from "./Overview";

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

interface Pop {
    rowId: string;
    payerId: string;
    kind: "refund" | "clarify";
}

export default function Invoices() {
    const {year, setYear, dataVersion} = useApp();
    const navigate = useNavigate();
    const toast = useToast();
    const confirm = useConfirm();
    const years = useBackend(() => Svc.Years(), [dataVersion]);
    const settings = useBackend(() => Svc.GetSettings(), [dataVersion]);
    const list = useBackend(() => Svc.ListInvoices(year), [year, dataVersion]);
    const [rows, setRows] = useState<InvoiceRowDTO[]>([]);
    const [filter, setFilter] = useState<Filter>("all");
    const [query, setQuery] = useState("");
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

    /** Opens the invoice when a row is clicked anywhere but on its checkboxes, buttons or links. */
    function openFromRow(e: React.MouseEvent, id: string) {
        const target = e.target as HTMLElement;
        if (target.closest("input, button, a, label, select, textarea, .popover, [role=dialog]")) return;
        if (window.getSelection()?.toString()) return; // the user is selecting text
        navigate(`/rechnung/${encodeURIComponent(id)}`);
    }

    async function change(ch: MarkChangeDTO) {
        try {
            const row = await Svc.SetMark(ch);
            setRows(rs => rs.map(r => (r.id === row.id ? row : r)));
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

    const sum = (pick: (r: InvoiceRowDTO) => string) => visible.reduce((s, r) => s + num(pick(r)), 0);
    const claimSum = (payerId: string, field: "expected" | "actual") =>
        visible.reduce((s, r) => s + num((r.claims ?? []).find(c => c.payerId === payerId)?.[field]), 0);

    const yearList = years.data ?? [year];
    return (
        <>
            <div className="page-head">
                <h1>Rechnungen</h1>
                <YearSelect years={yearList.includes(year) ? yearList : [year, ...yearList]} value={year}
                            onChange={setYear} allLabel="alle Jahre"/>
                <span className="spacer"/>
                <input ref={search} aria-label="Suchen" placeholder={`Suchen … (${key("F")})`} style={{width: 200}} value={query}
                       onChange={e => setQuery(e.target.value)}/>
                <button onClick={() => exportExcel(toast, year)}>Excel</button>
                <button className="primary" onClick={() => navigate("/rechnung/neu")}>Neue Rechnung</button>
            </div>
            <OutstandingCards year={year}/>
            <div className="chips" style={{marginBottom: 12}}>
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
                <BulkBar ids={[...selected]} payers={payers} onDone={() => list.reload()}/>
            )}
            {list.loading && !list.data ? <Loading/> : visible.length === 0 ? (
                <Empty>
                    {rows.length === 0
                        ? <>Für {year === 0 ? "diesen Zeitraum" : year} sind noch keine Rechnungen erfasst. <Link to="/rechnung/neu">Neue Rechnung</Link> oder <Link to="/import">Excel importieren</Link>.</>
                        : "Keine Rechnung passt zum Filter."}
                </Empty>
            ) : (
                <div className="table-wrap">
                    <table className="invoices">
                        <thead>
                        <tr>
                            <th className="check-col"><input type="checkbox" aria-label="Alle auswählen" checked={allSelected} onChange={toggleAll}/></th>
                            <th>Behandlung</th>
                            <th>Behandler</th>
                            <th className="num">Betrag</th>
                            <th>Bezahlt</th>
                            {payers.map(p => <th key={p.id} className="payer" colSpan={2}>{p.name}</th>)}
                            <th className="num" title="Rechnungsbetrag minus Erstattungen">Nicht erstattet</th>
                        </tr>
                        </thead>
                        <tbody>
                        {visible.map(r => (
                            <tr key={r.id} className={`clickable ${selected.has(r.id) ? "sel" : ""}`}
                                onClick={e => openFromRow(e, r.id)}>
                                <td><input type="checkbox" aria-label={`${r.provider} auswählen`} checked={selected.has(r.id)} onChange={() => toggleOne(r.id)}/></td>
                                <td>
                                    {date(r.serviceDate)}
                                    {r.invoiceDate !== r.serviceDate && <div className="sub">Rechnung {date(r.invoiceDate)}</div>}
                                </td>
                                <td>
                                    <Link className="provider-link" to={`/rechnung/${encodeURIComponent(r.id)}`}>{r.provider || "(ohne Namen)"}</Link>
                                    {"\u00a0"}<Link className="edit-link" to={`/rechnung/${encodeURIComponent(r.id)}`}
                                               aria-label={`${r.provider || "Rechnung"} bearbeiten`} title="Rechnung öffnen und bearbeiten">✎</Link>
                                    {(r.warnings ?? []).length > 0 && (
                                        <> <Badge kind="warn" title={(r.warnings ?? []).map(w => w.text).join("\n")}>⚠ {(r.warnings ?? []).length}</Badge></>
                                    )}
                                    {r.note.trim() !== "" && (
                                        <> <span className="badge note-badge" title={r.note} aria-label={`Notiz: ${r.note}`}>Notiz</span></>
                                    )}
                                    <div className="sub">{[CATEGORY_LABELS[r.category], r.reference].filter(Boolean).join(" · ")}</div>
                                </td>
                                <td className="num">{eur(r.amount)}</td>
                                <td>
                                    <MarkBox label="bezahlt" mark={r.paid}
                                             onToggle={on => change({invoiceId: r.id, payerId: "", mark: "paid", on, date: "", note: "", amount: ""})}
                                             onDate={d => change({invoiceId: r.id, payerId: "", mark: "paid", on: true, date: d, note: "", amount: ""})}/>
                                </td>
                                {payers.map(p => {
                                    const c = (r.claims ?? []).find(x => x.payerId === p.id);
                                    if (!c) return <td key={p.id} colSpan={2} className="payer-first muted small">–</td>;
                                    return (
                                        <ClaimCells key={p.id} row={r} claim={c} pop={pop} setPop={setPop} change={change}
                                                    toggleRefunded={on => toggleRefunded(r, c, on)}
                                                    toggleClarify={() => toggleClarify(r, c)}/>
                                    );
                                })}
                                <td className="num">{eur(r.ownShare)}</td>
                            </tr>
                        ))}
                        </tbody>
                        <tfoot>
                        <tr>
                            <td/>
                            <td colSpan={2}>{plural(visible.length, "Rechnung", "Rechnungen")}</td>
                            <td className="num">{eur(sum(r => r.amount))}</td>
                            <td/>
                            {payers.map(p => (
                                <td key={p.id} className="amounts payer-first" colSpan={2}>
                                    SOLL {eur(claimSum(p.id, "expected"))} · IST {eur(claimSum(p.id, "actual"))}
                                </td>
                            ))}
                            <td className="num">{eur(sum(r => r.ownShare))}</td>
                        </tr>
                        </tfoot>
                    </table>
                </div>
            )}
        </>
    );
}

function ClaimCells({row, claim: c, pop, setPop, change, toggleRefunded, toggleClarify}: {
    row: InvoiceRowDTO;
    claim: ClaimViewDTO;
    pop: Pop | null;
    setPop: (p: Pop | null) => void;
    change: (ch: MarkChangeDTO) => Promise<void>;
    toggleRefunded: (on: boolean) => void;
    toggleClarify: () => void;
}) {
    const base = {invoiceId: row.id, payerId: c.payerId, note: "", amount: ""};
    const open = pop?.rowId === row.id && pop.payerId === c.payerId ? pop.kind : null;
    const diff = num(c.diff);
    return (
        <>
            <td className="amounts payer-first" title={`Satz ${c.rate} %`}>
                <span className="muted small">SOLL</span> {eur(c.expected)}
                <span className="ist">
                    <span className="muted small">IST</span> {c.status === "refunded" ? eur(c.actual) : "–"}
                    {c.status === "refunded" && diff !== 0 && <span className={`small ${diff < 0 ? "neg" : "pos"}`}> ({eur(c.diff)})</span>}
                </span>
            </td>
            <td className="rel">
                <div className="markstack">
                    <span className="markline">
                        <MarkBox label="eingereicht" mark={c.submitted}
                                 onToggle={on => change({...base, mark: "submitted", on, date: ""})}
                                 onDate={d => change({...base, mark: "submitted", on: true, date: d})}/>
                        <button type="button" className={`flag ${c.clarify.done ? "on" : c.suggestClarify ? "suggest" : ""}`}
                                aria-pressed={c.clarify.done}
                                aria-label={`Klärungsbedarf ${c.payerName}`}
                                title={c.clarify.done ? `Klärungsbedarf seit ${date(c.clarify.date)}${c.clarify.note ? `: ${c.clarify.note}` : ""}`
                                    : c.suggestClarify ? "IST weicht vom SOLL ab – Klärungsbedarf markieren?" : "Klärungsbedarf markieren"}
                                onClick={toggleClarify}>⚑</button>
                    </span>
                    <MarkBox label="erstattet" mark={c.refunded.done || num(c.actual) !== 0 ? {...c.refunded, done: true} : c.refunded}
                             onToggle={toggleRefunded}/>
                    <span className="sub">
                        {c.held && "zurückgehalten "}
                        {c.skip && "verzichtet "}
                        {c.deadline && c.deadlineDays <= 90 && (
                            <span className={c.deadlineDays < 0 ? "neg" : ""}>
                                {c.deadlineDays < 0 ? `Frist abgelaufen (${date(c.deadline)})` : `Frist bis ${date(c.deadline)}`}
                            </span>
                        )}
                    </span>
                </div>
                {open === "refund" && (
                    <RefundPopover claim={c} onCancel={() => setPop(null)}
                                   onSave={(amount, d) => change({...base, mark: "refunded", on: true, date: d, amount})}/>
                )}
                {open === "clarify" && (
                    <ClarifyPopover onCancel={() => setPop(null)}
                                    onSave={note => change({...base, mark: "clarify", on: true, date: "", note})}/>
                )}
            </td>
        </>
    );
}

function RefundPopover({claim, onSave, onCancel}: {claim: ClaimViewDTO; onSave: (amount: string, date: string) => void; onCancel: () => void}) {
    const open = num(claim.expected) - num(claim.actual);
    const [amount, setAmount] = useState(toInput((open > 0 ? open : num(claim.expected)).toFixed(2)));
    const [d, setD] = useState(today());
    const ref = useDialog<HTMLDivElement>(onCancel);
    return (
        <div ref={ref} className="popover" role="dialog" aria-label={`Erstattung ${claim.payerName}`} tabIndex={-1}>
            <div className="grid" style={{gridTemplateColumns: "1fr 1fr"}}>
                <div className="field">
                    <label htmlFor="refund-amount">Erstattet (€)</label>
                    <input id="refund-amount" value={amount} onChange={e => setAmount(e.target.value)}
                           onKeyDown={e => e.key === "Enter" && onSave(amount, d)}/>
                </div>
                <div className="field">
                    <label htmlFor="refund-date">am</label>
                    <input id="refund-date" type="date" value={d} onChange={e => setD(e.target.value)}/>
                </div>
            </div>
            <div className="hint small muted" style={{marginTop: 4}}>SOLL {eur(claim.expected)}</div>
            <div className="actions" style={{marginTop: 10}}>
                <span className="spacer"/>
                <button onClick={onCancel}>Abbrechen</button>
                <button className="primary" onClick={() => onSave(amount, d)}>Übernehmen</button>
            </div>
        </div>
    );
}

function ClarifyPopover({onSave, onCancel}: {onSave: (note: string) => void; onCancel: () => void}) {
    const [note, setNote] = useState("");
    const ref = useDialog<HTMLDivElement>(onCancel);
    return (
        <div ref={ref} className="popover" role="dialog" aria-label="Klärungsbedarf" tabIndex={-1}>
            <div className="field">
                <label htmlFor="clarify-note">Klärungsbedarf – Grund</label>
                <input id="clarify-note" value={note} placeholder="z. B. Steigerungssatz gekürzt"
                       onChange={e => setNote(e.target.value)} onKeyDown={e => e.key === "Enter" && onSave(note)}/>
            </div>
            <div className="actions" style={{marginTop: 10}}>
                <span className="spacer"/>
                <button onClick={onCancel}>Abbrechen</button>
                <button className="primary" onClick={() => onSave(note)}>Markieren</button>
            </div>
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

/**
 * What each payer still owes: SOLL − IST of the claims that are submitted or
 * still to be submitted, for the chosen year or all years (calc.Outstanding).
 */
function OutstandingCards({year}: {year: number}) {
    const {dataVersion} = useApp();
    const figures = useBackend<YearDTO>(() => (year === 0 ? Svc.Overview().then(o => o.total) : Svc.Dashboard(year)), [year, dataVersion]);
    const payers = (figures.data?.payers ?? []).filter(p => p.claims > 0);
    if (payers.length === 0) return null;
    const period = year === 0 ? "alle Jahre" : String(year);
    return (
        <div className="cards outstanding">
            {payers.map(p => {
                const o = p.outstanding;
                const parts = [
                    num(o.submitted) !== 0 && `eingereicht, offen ${eur(o.submitted)}`,
                    num(o.open) !== 0 && `noch nicht eingereicht ${eur(o.open)}`,
                    num(o.refunded) !== 0 && `Abweichung bei erstatteten ${eur(o.refunded)}`,
                ].filter(Boolean);
                return (
                    <div key={p.payerId} className="card">
                        <div className="label">{p.payerName} · Ausstand {period}</div>
                        <div className="value">{eur(o.total)}</div>
                        <div className="note">SOLL {eur(o.expected)} − IST {eur(o.actual)}</div>
                        {parts.length > 0 && <div className="note">davon {parts.join(" · ")}</div>}
                        {num(o.held) !== 0 && <div className="note">zurückgehalten {eur(o.held)} – nicht eingerechnet</div>}
                    </div>
                );
            })}
        </div>
    );
}
