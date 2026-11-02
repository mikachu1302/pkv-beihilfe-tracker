import {useEffect, useState} from "react";
import {Link} from "react-router";
import {Svc} from "../api";
import type {AllowanceDTO, PayerYearDTO, YearSettingDTO} from "../api";
import {useApp} from "../App";
import {useConfirm, useToast} from "../components/feedback";
import {SegmentBar} from "../components/charts";
import type {Segment} from "../components/charts";
import {ErrorNotice, Loading, Notice, Stat, YearSelect} from "../components/ui";
import {date, eur, num, plural, toInput, today} from "../lib/format";
import {useBackend} from "../lib/useBackend";

const BUCKETS: {key: "refunded" | "submitted" | "open" | "held" | "skipped"; label: string; slot: number}[] = [
    {key: "refunded", label: "Erstattet", slot: 1},
    {key: "submitted", label: "Eingereicht, Erstattung offen", slot: 2},
    {key: "open", label: "Nicht eingereicht", slot: 3},
    {key: "held", label: "Zurückgehalten", slot: 4},
    {key: "skipped", label: "Wird nicht eingereicht", slot: 5},
];

export default function Dashboard() {
    const {year, setYear, dataVersion} = useApp();
    const years = useBackend(() => Svc.Years(), [dataVersion]);
    const dash = useBackend(() => Svc.Dashboard(year), [year, dataVersion]);
    const yearList = years.data ?? [year];
    const y = dash.data;
    return (
        <>
            <div className="page-head">
                <h1>Dashboard</h1>
                <YearSelect years={yearList.includes(year) ? yearList : [year, ...yearList]} value={year} onChange={setYear}/>
                <span className="muted small">Leistungsjahr: Jahr der Behandlung</span>
            </div>
            {dash.error && <ErrorNotice error={dash.error}/>}
            {!y ? <Loading/> : (
                <div style={{opacity: dash.loading ? 0.6 : 1}}>
                    <div className="cards">
                        <Stat label="Rechnungen" value={y.invoices.toLocaleString("de-DE")}
                              note={<Link to="/rechnungen">anzeigen</Link>}/>
                        <Stat label="Rechnungsbeträge" value={eur(y.billed)}/>
                        <Stat label="Erstattet (IST)" value={eur(y.actual)} note={`SOLL ${eur(y.expected)}`}/>
                        <Stat label="Noch nicht erstattet" value={eur(y.ownShare)}
                              note={`endgültiger Eigenanteil abgeschlossener Rechnungen: ${eur(y.ownShareSettled)}`}/>
                        {num(y.allowances) > 0 && <Stat label="Leistungen ohne Rechnung" value={eur(y.allowances)} note="z. B. Krankenhaustagegeld"/>}
                    </div>
                    {(y.payers ?? []).map(p => <PayerSection key={p.payerId} year={year} p={p} onSaved={dash.reload}/>)}
                    <Allowances year={year} payers={y.payers ?? []} onChanged={dash.reload}/>
                </div>
            )}
        </>
    );
}

function PayerSection({year, p, onSaved}: {year: number; p: PayerYearDTO; onSaved: () => void}) {
    const segments: Segment[] = BUCKETS.map(b => ({key: b.key, label: b.label, value: num(p[b.key].expected), slot: b.slot}));
    const isPKV = p.kind === "pkv";
    const openBilled = num(p.openBilled);
    const minClaim = num(p.minClaimAmount);
    return (
        <section className="panel" aria-label={p.payerName}>
            <div className="payer-head">
                <h2>{p.payerName}</h2>
                <span className="muted">{isPKV ? "Private Krankenversicherung" : "Beihilfe"} · {plural(p.claims, "Rechnung", "Rechnungen")}</span>
            </div>
            <div className="cards" style={{marginTop: 12}}>
                <Stat label={isPKV ? "Müsste zahlen (SOLL)" : "Müsste erstatten (SOLL)"} value={eur(p.expected)}/>
                <Stat label="Erstattet (IST)" value={eur(p.actual)}/>
                <Stat label="Abweichung erstatteter Rechnungen" value={eur(p.deviation)}
                      tone={num(p.deviation) < 0 ? "neg" : undefined} note="IST − SOLL"/>
            </div>
            <SegmentBar segments={segments} label={`${p.payerName}: SOLL nach Status`}/>
            <table>
                <thead>
                <tr><th>Status</th><th className="num">Rechnungen</th><th className="num">SOLL</th><th className="num">IST</th></tr>
                </thead>
                <tbody>
                {BUCKETS.filter(b => p[b.key].count > 0 || b.key !== "skipped").map(b => (
                    <tr key={b.key}>
                        <td><span className={`swatch s${b.slot}`}/>{b.label}</td>
                        <td className="num">{p[b.key].count}</td>
                        <td className="num">{eur(p[b.key].expected)}</td>
                        <td className="num">{b.key === "refunded" ? eur(p[b.key].actual) : "–"}</td>
                    </tr>
                ))}
                </tbody>
            </table>
            <ul className="hint-list">
                {p.clarify > 0 && <li>{plural(p.clarify, "Rechnung", "Rechnungen")} mit Klärungsbedarf</li>}
                {p.suggestClarify > 0 && <li>{plural(p.suggestClarify, "Erstattung weicht", "Erstattungen weichen")} vom SOLL ab und {p.suggestClarify === 1 ? "ist" : "sind"} noch nicht als Klärungsbedarf markiert</li>}
                {p.deadlineMissed > 0 && <li className="neg">Antragsfrist bei {plural(p.deadlineMissed, "Rechnung", "Rechnungen")} abgelaufen</li>}
                {p.deadlineSoon > 0 && <li>Antragsfrist bei {plural(p.deadlineSoon, "Rechnung", "Rechnungen")} in den nächsten 90 Tagen</li>}
                {!isPKV && minClaim > 0 && p.open.count > 0 && (
                    <li>Noch nicht eingereicht: Rechnungen über {eur(openBilled)} – {openBilled > minClaim
                        ? `über dem Mindestbetrag von ${eur(minClaim)}, ein Antrag ist möglich`
                        : `noch unter dem Mindestbetrag von ${eur(minClaim)}`}</li>
                )}
            </ul>
            {year > 0 && (isPKV ? <PremiumRefund year={year} p={p} onSaved={onSaved}/> : (
                <div className="year-values"><YearValue label={`Kostendämpfungspauschale ${year}`} year={year} p={p} field="annualDeduction" onSaved={onSaved}
                           hint="Jährlicher Eigenanteil der Beihilfe (nicht in allen Ländern)"/></div>
            ))}
        </section>
    );
}

function PremiumRefund({year, p, onSaved}: {year: number; p: PayerYearDTO; onSaved: () => void}) {
    const pr = p.premiumRefund;
    const adv = num(pr?.advantage);
    return (
        <div style={{marginTop: 16}}>
            <h3>Einreichen oder Beitragsrückerstattung?</h3>
            <div className="row" style={{gap: 24, alignItems: "flex-start"}}>
                <YearValue label={`Erwartete Beitragsrückerstattung ${year}`} year={year} p={p} field="expectedPremiumRefund" onSaved={onSaved}
                           hint="Betrag laut Tarif, wenn für das Jahr nichts eingereicht wird"/>
                <YearValue label={`Selbstbehalt ${year}`} year={year} p={p} field="deductible" onSaved={onSaved}/>
            </div>
            {!pr ? (
                <p className="muted small">Erwartete Beitragsrückerstattung eintragen, um Einreichen und Nichteinreichen zu vergleichen.</p>
            ) : (
                <>
                    <div className="cards" style={{marginTop: 12}}>
                        <Stat label="Alles einreichen: PKV zahlt" value={eur(pr.claimAll)}
                              note={num(p.deductible) > 0 ? `nach Selbstbehalt von ${eur(p.deductible)}` : undefined}/>
                        <Stat label="Nicht einreichen: Rückerstattung" value={eur(pr.holdTotal)}
                              note={num(pr.claimNeutralOnly) > 0 ? `inkl. ${eur(pr.claimNeutralOnly)} für Rechnungen ohne Einfluss auf die Rückerstattung` : undefined}/>
                    </div>
                    <p className="verdict">
                        {adv > 0 && <>Einreichen lohnt sich: <strong>{eur(adv)}</strong> mehr als die Beitragsrückerstattung.</>}
                        {adv < 0 && <>Nicht einreichen lohnt sich: die Beitragsrückerstattung bringt <strong>{eur(-adv)}</strong> mehr.</>}
                        {adv === 0 && <>Beide Wege ergeben gleich viel.</>}
                    </p>
                    {pr.forfeited && (
                        <Notice kind="warn">Für {year} ist bereits eine Rechnung bei der PKV eingereicht oder erstattet – die Beitragsrückerstattung entfällt dann in der Regel.</Notice>
                    )}
                </>
            )}
        </div>
    );
}

function YearValue({label, year, p, field, hint, onSaved}: {
    label: string;
    year: number;
    p: PayerYearDTO;
    field: "deductible" | "annualDeduction" | "expectedPremiumRefund";
    hint?: string;
    onSaved: () => void;
}) {
    const toast = useToast();
    const stored = toInput(p.setting?.[field] ?? "");
    const [value, setValue] = useState(stored);
    useEffect(() => setValue(stored), [stored]);
    const id = `${p.payerId}-${field}`;

    async function save() {
        if (value === stored) return;
        const setting: YearSettingDTO = {
            year, payerId: p.payerId,
            deductible: toInput(p.setting?.deductible ?? ""),
            annualDeduction: toInput(p.setting?.annualDeduction ?? ""),
            expectedPremiumRefund: toInput(p.setting?.expectedPremiumRefund ?? ""),
            [field]: value,
        };
        try {
            await Svc.SaveYearSetting(setting);
            onSaved();
        } catch (e) {
            toast.error(e);
        }
    }

    return (
        <div className="field">
            <label htmlFor={id}>{label}</label>
            <div className="inline-input">
                <input id={id} inputMode="decimal" placeholder="0,00" value={value} onChange={e => setValue(e.target.value)}
                       onBlur={save} onKeyDown={e => e.key === "Enter" && e.currentTarget.blur()}/>
                <span className="muted">€</span>
            </div>
            {hint && <div className="hint">{hint}</div>}
        </div>
    );
}

function Allowances({year, payers, onChanged}: {year: number; payers: PayerYearDTO[]; onChanged: () => void}) {
    const toast = useToast();
    const confirm = useConfirm();
    const {dataVersion} = useApp();
    const list = useBackend(() => Svc.ListAllowances(year), [year, dataVersion]);
    const pkvs = payers.filter(p => p.kind === "pkv");
    const [draft, setDraft] = useState<AllowanceDTO | null>(null);

    function start() {
        setDraft({id: "", payerId: pkvs[0]?.payerId ?? payers[0]?.payerId ?? "", kind: "krankenhaustagegeld",
            date: year === new Date().getFullYear() ? today() : `${year}-01-01`, amount: "",
            received: {done: false, date: "", note: ""}, note: ""});
    }

    async function save() {
        if (!draft) return;
        try {
            await Svc.SaveAllowance(draft);
            setDraft(null);
            list.reload();
            onChanged();
        } catch (e) {
            toast.error(e);
        }
    }

    async function remove(a: AllowanceDTO) {
        if (!await confirm(`${eur(a.amount)} vom ${date(a.date)} löschen?`, "Löschen")) return;
        try {
            await Svc.DeleteAllowance(a.id);
            list.reload();
            onChanged();
        } catch (e) {
            toast.error(e);
        }
    }

    const items = list.data ?? [];
    return (
        <section className="panel" aria-label="Leistungen ohne Rechnung">
            <div className="payer-head">
                <h2>Leistungen ohne Rechnung</h2>
                <span className="muted">z. B. Krankenhaustagegeld</span>
                <span className="spacer"/>
                {!draft && <button onClick={start}>+ Eintrag</button>}
            </div>
            {items.length === 0 && !draft && <p className="muted small" style={{marginTop: 8}}>Keine Einträge für {year}.</p>}
            {(items.length > 0 || draft) && (
                <table style={{marginTop: 8}}>
                    <thead><tr><th>Datum</th><th>Kostenträger</th><th>Notiz</th><th className="num">Betrag</th><th/></tr></thead>
                    <tbody>
                    {items.map(a => (
                        <tr key={a.id}>
                            <td>{date(a.date)}</td>
                            <td>{payers.find(p => p.payerId === a.payerId)?.payerName ?? a.payerId}</td>
                            <td>{a.note || "Krankenhaustagegeld"}</td>
                            <td className="num">{eur(a.amount)}</td>
                            <td><button className="link" aria-label="Eintrag löschen" onClick={() => remove(a)}>✕</button></td>
                        </tr>
                    ))}
                    {draft && (
                        <tr>
                            <td><input type="date" aria-label="Datum" value={draft.date} onChange={e => setDraft({...draft, date: e.target.value})}/></td>
                            <td>
                                <select aria-label="Kostenträger" value={draft.payerId} onChange={e => setDraft({...draft, payerId: e.target.value})}>
                                    {payers.map(p => <option key={p.payerId} value={p.payerId}>{p.payerName}</option>)}
                                </select>
                            </td>
                            <td><input aria-label="Notiz" value={draft.note} placeholder="Krankenhaustagegeld" onChange={e => setDraft({...draft, note: e.target.value})}/></td>
                            <td><input aria-label="Betrag" inputMode="decimal" value={draft.amount} onChange={e => setDraft({...draft, amount: e.target.value})}/></td>
                            <td>
                                <span className="row" style={{flexWrap: "nowrap"}}>
                                    <button className="primary" onClick={save}>OK</button>
                                    <button onClick={() => setDraft(null)}>✕</button>
                                </span>
                            </td>
                        </tr>
                    )}
                    </tbody>
                </table>
            )}
        </section>
    );
}
