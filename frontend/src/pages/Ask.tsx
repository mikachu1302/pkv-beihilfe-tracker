// Fragen: questions about the own invoices. The AI turns a question into a
// query (internal/ask), the app computes the answer – the invoices never go
// to the AI. How the app understood the question shows as chips that can be
// changed or removed, without asking the AI again.
import {useEffect, useRef, useState} from "react";
import type {FormEvent} from "react";
import {Link, useNavigate} from "react-router";
import {Svc} from "../api";
import type {AnswerDTO, AskVocabularyDTO, ChoiceDTO, QueryDTO, TurnDTO} from "../api";
import {useApp} from "../App";
import {Notice} from "../components/ui";
import {useAIOn} from "../lib/ai";
import {date, errMsg, eur, STATUS_LABELS} from "../lib/format";
import {invoicePath} from "../lib/paths";
import {createStore, useStore} from "../lib/store";
import {useBackend} from "../lib/useBackend";
import type {Filter, ListState} from "./Invoices";

interface Entry {
    id: number;
    question: string;
    answer?: AnswerDTO;
    error?: string;
}

// The questions of this session (kept while the app runs, not stored).
const entries = createStore<Entry[]>([]);
let nextId = 1;

/** Only for tests: forget the questions of the session. */
export function clearQuestions() {
    entries.set([]);
}

const EXAMPLES = [
    "Wie viele Rechnungen aus diesem Jahr sind noch offen?",
    "Wo hat die Beihilfe zu wenig erstattet?",
    "Wie viel habe ich dieses Jahr beim Zahnarzt ausgegeben?",
    "Was steht insgesamt noch aus?",
];

/** The invoices shown below an answer before "alle zeigen". */
const SHOWN = 10;

export default function Ask() {
    const aiOn = useAIOn();
    const {dataVersion} = useApp();
    const list = useStore(entries);
    const vocabulary = useBackend(() => Svc.AskVocabulary(), [dataVersion]);
    const [question, setQuestion] = useState("");
    const [running, setRunning] = useState<{cancel: () => void} | null>(null);
    const input = useRef<HTMLInputElement>(null);
    const end = useRef<HTMLDivElement>(null);
    useEffect(() => () => running?.cancel(), [running]);
    useEffect(() => end.current?.scrollIntoView?.({block: "nearest"}), [list.length]);

    const update = (id: number, patch: Partial<Entry>) => entries.set(entries.get().map(e => (e.id === id ? {...e, ...patch} : e)));

    async function ask(text: string) {
        const q = text.trim();
        if (!q || running) return;
        // The last questions with the queries as they stand now (chips changed by hand included).
        const history: TurnDTO[] = entries.get().filter(e => e.answer).slice(-2)
            .map(e => ({frage: e.question, abfrage: e.answer!.query}));
        const entry: Entry = {id: nextId++, question: q};
        entries.set([...entries.get(), entry]);
        setQuestion("");
        const p = Svc.Ask({question: q, history});
        let cancelled = false;
        setRunning({cancel: () => {
            cancelled = true;
            p.cancel();
        }});
        try {
            update(entry.id, {answer: await p});
        } catch (e) {
            update(entry.id, {error: cancelled ? "Abgebrochen." : errMsg(e)});
        } finally {
            setRunning(null);
            input.current?.focus();
        }
    }

    async function rerun(id: number, query: QueryDTO) {
        try {
            update(id, {answer: await Svc.RunQuery(query), error: undefined});
        } catch (e) {
            update(id, {error: errMsg(e)});
        }
    }

    const submit = (e: FormEvent) => {
        e.preventDefault();
        void ask(question);
    };

    return (
        <div className="ask">
            <div className="page-head">
                <h1>Fragen</h1>
                <span className="muted small">Die KI übersetzt die Frage, die App rechnet – die Rechnungen gehen nicht an die KI.</span>
                <span className="spacer"/>
                {list.length > 0 && <button className="link" onClick={clearQuestions}>Verlauf leeren</button>}
            </div>
            {!aiOn && (
                <Notice kind="warn">
                    Die KI ist ausgeschaltet – einschalten unter <Link to="/einstellungen">Einstellungen → KI</Link>. Ohne KI
                    helfen die Filter der <Link to="/rechnungen">Rechnungsliste</Link>.
                </Notice>
            )}
            {list.length === 0 && aiOn && (
                <div className="panel">
                    <p className="muted">Fragen zu den eigenen Rechnungen und Erstattungen, zum Beispiel:</p>
                    <div className="ask-examples">
                        {EXAMPLES.map(x => <button key={x} className="chip" onClick={() => void ask(x)}>{x}</button>)}
                    </div>
                </div>
            )}
            <div className="ask-log" aria-live="polite">
                {list.map(e => (
                    <section key={e.id} className="ask-entry" aria-label={e.question}>
                        <p className="ask-question">{e.question}</p>
                        {e.answer && <AnswerView answer={e.answer} vocabulary={vocabulary.data} onChange={q => void rerun(e.id, q)}/>}
                        {e.error && <Notice kind="warn">{e.error}{/nicht erreichbar/.test(e.error) && " Ohne KI helfen die Filter der Rechnungsliste."}</Notice>}
                        {!e.answer && !e.error && (
                            <p className="muted small" role="status">
                                Frage wird verstanden … <button className="link" onClick={() => running?.cancel()}>Abbrechen</button>
                            </p>
                        )}
                    </section>
                ))}
                <div ref={end}/>
            </div>
            <form className="ask-input" onSubmit={submit}>
                <input ref={input} aria-label="Frage" value={question} disabled={!aiOn} maxLength={500}
                       placeholder="z. B. Was hat die PKV 2025 erstattet?" onChange={e => setQuestion(e.target.value)}/>
                <button className="primary" type="submit" disabled={!aiOn || running !== null || !question.trim()}>Fragen</button>
            </form>
        </div>
    );
}

/** One answer: the chips, the sentence, figures, groups and the invoices. */
function AnswerView({answer: a, vocabulary, onChange}: {
    answer: AnswerDTO;
    vocabulary?: AskVocabularyDTO;
    onChange: (q: QueryDTO) => void;
}) {
    const [all, setAll] = useState(false);
    const navigate = useNavigate();
    const {setYear} = useApp();
    const invoices = a.invoices ?? [];
    const shown = all ? invoices : invoices.slice(0, SHOWN);
    const list = listState(a.query);
    return (
        <div className="panel ask-answer">
            <Chips answer={a} vocabulary={vocabulary} onChange={onChange}/>
            <p className="ask-sentence"><strong>{a.answer}</strong></p>
            {(a.figures ?? []).length > 0 && (
                <dl className="kv ask-figures">
                    {(a.figures ?? []).map(f => <div key={f.label}><dt>{f.label}</dt><dd className="mono">{f.value}</dd></div>)}
                </dl>
            )}
            {(a.groups ?? []).length > 0 && (
                <table className="ask-groups">
                    <tbody>
                    {(a.groups ?? []).map(g => (
                        <tr key={g.label}>
                            <td>{g.label}</td>
                            <td className="num muted">{g.count === 1 ? "1 Rechnung" : `${g.count} Rechnungen`}</td>
                            {g.value && <td className="num mono">{g.value}</td>}
                        </tr>
                    ))}
                    </tbody>
                </table>
            )}
            {invoices.length > 0 && (
                <>
                    <table className="ask-invoices">
                        <thead><tr><th>Behandlung</th><th>Behandler</th><th className="num">Betrag</th><th>Stand</th></tr></thead>
                        <tbody>
                        {shown.map(r => (
                            <tr key={r.id}>
                                <td>{date(r.serviceDate)}</td>
                                <td><Link to={invoicePath(r.id)}>{r.provider || "(ohne Behandler)"}</Link></td>
                                <td className="num mono">{eur(r.amount)}</td>
                                <td className="muted small">{(r.claims ?? []).map(c => `${c.payerName}: ${STATUS_LABELS[c.status] ?? c.status}`).join(" · ")}</td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                    <div className="actions">
                        {invoices.length > SHOWN && !all && <button className="link" onClick={() => setAll(true)}>Alle {invoices.length} zeigen</button>}
                        <button className="link" title={list.missing.length ? `Die Liste filtert nicht nach: ${list.missing.join(", ")}` : undefined}
                                onClick={() => {
                                    setYear(list.year);
                                    navigate("/rechnungen", {state: list.state});
                                }}>
                            In der Rechnungsliste zeigen
                        </button>
                    </div>
                </>
            )}
            {a.seconds > 0 && <p className="muted small">Verstanden in {Math.round(a.seconds)} s.</p>}
        </div>
    );
}

/** The parts of the query: the kind fixed, the others changeable or removable. */
function Chips({answer: a, vocabulary, onChange}: {answer: AnswerDTO; vocabulary?: AskVocabularyDTO; onChange: (q: QueryDTO) => void}) {
    const q = a.query;
    const remove = (key: string) => {
        if (key === "jahre") onChange({...q, jahre: []});
        else if (key === "gruppierung") onChange({...q, gruppierung: ""});
        else onChange({...q, filter: (q.filter ?? []).filter(f => f.feld !== key)});
    };
    const setFilter = (key: string, value: string) =>
        onChange({...q, filter: (q.filter ?? []).map(f => (f.feld === key ? {...f, wert: value} : f))});
    const choices = (key: string): ChoiceDTO[] | undefined => {
        switch (key) {
            case "kostentraeger": return vocabulary?.payers ?? undefined;
            case "status": return vocabulary?.statuses ?? undefined;
            case "kategorie": return vocabulary?.categories ?? undefined;
            case "gruppierung": return vocabulary?.groups ?? undefined;
        }
        return undefined;
    };
    const current = (key: string) => (key === "gruppierung" ? q.gruppierung : (q.filter ?? []).find(f => f.feld === key)?.wert ?? "");
    return (
        <div className="ask-chips" aria-label="So hat die App die Frage verstanden">
            {(a.chips ?? []).map(c => {
                const options = choices(c.key);
                const years = c.key === "jahre" ? vocabulary?.years ?? [] : [];
                return (
                    <span key={c.key} className={`chip ask-chip ${c.key === "typ" ? "fixed" : ""}`}>
                        {options ? (
                            <select aria-label={`${c.text} ändern`} value={current(c.key)}
                                    onChange={e => (c.key === "gruppierung" ? onChange({...q, gruppierung: e.target.value}) : setFilter(c.key, e.target.value))}>
                                {options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
                            </select>
                        ) : years.length > 0 && (q.jahre ?? []).length === 1 ? (
                            <select aria-label="Jahr ändern" value={(q.jahre ?? [])[0]} onChange={e => onChange({...q, jahre: [Number(e.target.value)]})}>
                                {years.filter(y => y > 0).map(y => <option key={y} value={y}>{y}</option>)}
                            </select>
                        ) : c.text}
                        {c.key !== "typ" && (
                            <button type="button" className="chip-remove" aria-label={`„${c.text}“ entfernen`} title="Entfernen"
                                    onClick={() => remove(c.key)}>×</button>
                        )}
                    </span>
                );
            })}
        </div>
    );
}

/**
 * The invoice list with what of the query it can show: one year (or all),
 * one of its filters, the provider as search. missing names the rest.
 */
export function listState(q: QueryDTO): {year: number; state: ListState; missing: string[]} {
    const years = q.jahre ?? [];
    const value = (field: string) => (q.filter ?? []).find(f => f.feld === field)?.wert ?? "";
    const filters: [Filter, boolean][] = [
        ["open", value("status") === "nicht_eingereicht"],
        ["submitted", value("status") === "eingereicht"],
        ["clarify", value("klaerungsbedarf") === "ja"],
        ["deadline", value("frist_bald") === "ja"],
        ["unpaid", value("bezahlt") === "nein"],
    ];
    const filter = filters.find(([, on]) => on)?.[0];
    const state: ListState = {filter, search: value("behandler") || undefined};
    const shown = new Set(["behandler", ...(filter ? [{open: "status", submitted: "status", clarify: "klaerungsbedarf",
        deadline: "frist_bald", unpaid: "bezahlt", all: "", warnings: ""}[filter]] : [])]);
    const names: Record<string, string> = {kostentraeger: "Kostenträger", status: "Status", abweichung: "Abweichung",
        klaerungsbedarf: "Klärungsbedarf", bezahlt: "bezahlt", frist_bald: "Frist", kategorie: "Kategorie", von: "Zeitraum", bis: "Zeitraum"};
    const missing = [...new Set((q.filter ?? []).filter(f => !shown.has(f.feld)).map(f => names[f.feld] ?? f.feld))];
    if (years.length > 1) missing.push("mehrere Jahre");
    return {year: years.length === 1 ? years[0] : 0, state, missing};
}
