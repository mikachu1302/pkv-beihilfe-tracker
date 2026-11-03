// Fragen: questions about the own invoices. The AI turns a question into a
// query (internal/ask), the app computes the answer – the invoices never go
// to the AI. How the app understood the question shows as chips that can be
// changed or removed, without asking the AI again.
import {useRef, useState} from "react";
import type {FormEvent} from "react";
import {Link} from "react-router";
import {Svc} from "../api";
import type {AnswerDTO, AskVocabularyDTO, ChoiceDTO, QueryDTO, TurnDTO} from "../api";
import {useApp} from "../App";
import {Notice} from "../components/ui";
import {AI_UNREACHABLE, useAIOn} from "../lib/ai";
import {date, errMsg, eur, plural, sentence, STATUS_LABELS} from "../lib/format";
import {invoicePath} from "../lib/paths";
import {createStore, useStore} from "../lib/store";
import {useBackend} from "../lib/useBackend";
import {Cancelled, useCancellable} from "../lib/useCancellable";
import type {Filter, ListState} from "./Invoices";

interface Entry {
    id: number;
    question: string;
    answer?: AnswerDTO;
    // The query as last changed by its chips (the answer to it may still be on its way).
    query?: QueryDTO;
    error?: string;
}

// The questions of this session (kept while the app runs, not stored).
const entries = createStore<Entry[]>([]);
let nextId = 1;

/** Forgets the questions of the session. */
export function clearQuestions() {
    entries.set([]);
}

const EXAMPLES = [
    "Wie viele Rechnungen aus diesem Jahr sind noch offen?",
    "Wo hat die Beihilfe zu wenig erstattet?",
    "Wie viel habe ich dieses Jahr beim Zahnarzt ausgegeben?",
    "Was steht insgesamt noch aus?",
];

/** The invoices shown below an answer before "Alle zeigen". */
const SHOWN = 10;

export default function Ask() {
    const aiOn = useAIOn();
    const {dataVersion} = useApp();
    const list = useStore(entries);
    const vocabulary = useBackend(() => Svc.AskVocabulary(), [dataVersion]);
    const [question, setQuestion] = useState("");
    const call = useCancellable();
    // What screen readers hear: the answer sentence only, not chips and tables.
    const [announce, setAnnounce] = useState("");
    const form = useRef<HTMLFormElement>(null);
    const input = useRef<HTMLInputElement>(null);
    // Chip changes per entry: only the answer to the latest counts.
    const reruns = useRef(new Map<number, number>());

    const update = (id: number, patch: Partial<Entry>) => entries.set(entries.get().map(e => (e.id === id ? {...e, ...patch} : e)));

    async function ask(text: string) {
        const q = text.trim();
        if (!q || call.running) return;
        // The last questions with the queries as they stand now (chips changed by hand included).
        const history: TurnDTO[] = entries.get().filter(e => e.answer).slice(-2)
            .map(e => ({frage: e.question, abfrage: e.query ?? e.answer!.query}));
        const entry: Entry = {id: nextId++, question: q};
        entries.set([...entries.get(), entry]);
        setQuestion("");
        try {
            const answer = await call.run(() => Svc.Ask({question: q, history}));
            update(entry.id, {answer, query: answer.query});
            setAnnounce(answer.answer);
        } catch (e) {
            update(entry.id, {error: e instanceof Cancelled ? "Abgebrochen." : errMsg(e)});
        } finally {
            // Back to the input – unless the user went elsewhere meanwhile (e.g. to an earlier answer).
            const at = document.activeElement;
            if (!at || at === document.body || form.current?.contains(at)) input.current?.focus();
        }
    }

    async function rerun(id: number, query: QueryDTO) {
        const n = (reruns.current.get(id) ?? 0) + 1;
        reruns.current.set(id, n);
        update(id, {query});
        try {
            const answer = await Svc.RunQuery(query);
            if (reruns.current.get(id) !== n) return; // a later change is on its way
            update(id, {answer, error: undefined});
            setAnnounce(answer.answer);
        } catch (e) {
            if (reruns.current.get(id) === n) update(id, {error: errMsg(e)});
        }
    }

    function clear() {
        call.cancel();
        clearQuestions();
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
                {list.length > 0 && <button className="link" onClick={clear}>Verlauf leeren</button>}
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
            <p className="sr-only" aria-live="polite">{announce}</p>
            <div className="ask-log">
                {list.map(e => (
                    <article key={e.id} className="ask-entry" aria-label={e.question}>
                        <p className="ask-question">{e.question}</p>
                        {e.answer && <AnswerView answer={e.answer} query={e.query ?? e.answer.query} vocabulary={vocabulary.data}
                                                 onChange={q => void rerun(e.id, q)}/>}
                        {e.error && (
                            <Notice kind="warn">
                                {sentence(e.error)}{e.error.startsWith(AI_UNREACHABLE) && " Ohne KI helfen die Filter der Rechnungsliste."}
                            </Notice>
                        )}
                        {!e.answer && !e.error && (
                            <p className="muted small">
                                <span role="status">Frage wird verstanden …</span>{" "}
                                <button className="link" onClick={call.cancel}>Abbrechen</button>
                            </p>
                        )}
                    </article>
                ))}
            </div>
            <form ref={form} className="ask-input" onSubmit={submit}>
                <input ref={input} aria-label="Frage" value={question} disabled={!aiOn} maxLength={500}
                       placeholder="z. B. Was hat die PKV 2025 erstattet?" onChange={e => setQuestion(e.target.value)}/>
                <button className="primary" type="submit" disabled={!aiOn || call.running || !question.trim()}>Fragen</button>
            </form>
        </div>
    );
}

/** One answer: the chips, the sentence, figures, groups and the invoices. */
function AnswerView({answer: a, query, vocabulary, onChange}: {
    answer: AnswerDTO;
    query: QueryDTO;
    vocabulary?: AskVocabularyDTO;
    onChange: (q: QueryDTO) => void;
}) {
    const [all, setAll] = useState(false);
    const {setYear} = useApp();
    const invoices = a.invoices ?? [];
    const shown = all ? invoices : invoices.slice(0, SHOWN);
    const list = listState(a.query);
    // The list cannot show the Ausstand or the premium refund comparison.
    const toList = a.query.typ !== "ausstand" && a.query.typ !== "beitragsrueckerstattung";
    // Asked about one payer: only its state.
    const payer = (a.query.filter ?? []).find(f => f.feld === "kostentraeger")?.wert;
    return (
        <div className="panel ask-answer">
            <Chips answer={a} query={query} vocabulary={vocabulary} onChange={onChange}/>
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
                            <td className="num muted">{plural(g.count, "Rechnung", "Rechnungen")}</td>
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
                                <td className="muted small">{(r.claims ?? []).filter(c => !payer || c.payerName === payer)
                                    .map(c => `${c.payerName}: ${STATUS_LABELS[c.status] ?? c.status}`).join(" · ")}</td>
                            </tr>
                        ))}
                        </tbody>
                    </table>
                    <div className="actions">
                        {invoices.length > SHOWN && (
                            <button className="link" onClick={() => setAll(!all)}>
                                {all ? "Weniger zeigen" : `Alle ${invoices.length} zeigen`}
                            </button>
                        )}
                        {toList && (
                            <span>
                                <Link to="/rechnungen" state={list.state} onClick={() => setYear(list.year)}>In der Rechnungsliste zeigen</Link>
                                {list.missing.length > 0 && <span className="muted small"> (ohne Filter nach {list.missing.join(", ")})</span>}
                            </span>
                        )}
                    </div>
                </>
            )}
            {a.seconds > 0 && <p className="muted small">Verstanden in {Math.round(a.seconds)} s.</p>}
        </div>
    );
}

/** What a chip stands for, for the name of its choice ("Status ändern"). */
const CHIP_NAMES: Record<string, string> = {
    jahre: "Jahr", kostentraeger: "Kostenträger", status: "Status", abweichung: "Abweichung", klaerungsbedarf: "Klärungsbedarf",
    bezahlt: "bezahlt", frist_bald: "Frist", kategorie: "Kategorie", behandler: "Behandler", von: "Zeitraum", bis: "Zeitraum",
    gruppierung: "Gruppierung",
};

/** The parts of the query: the kind fixed, the others changeable or removable. */
function Chips({answer: a, query: q, vocabulary, onChange}: {
    answer: AnswerDTO;
    query: QueryDTO;
    vocabulary?: AskVocabularyDTO;
    onChange: (q: QueryDTO) => void;
}) {
    const group = useRef<HTMLDivElement>(null);
    const remove = (key: string) => {
        if (key === "jahre") onChange({...q, jahre: []});
        else if (key === "gruppierung") onChange({...q, gruppierung: ""});
        else onChange({...q, filter: (q.filter ?? []).filter(f => f.feld !== key)});
        group.current?.focus(); // the chip with the focused button goes away
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
    const queryYears = q.jahre ?? [];
    // The years with data, and the one asked about even if it has none.
    const years = [...new Set([...(vocabulary?.years ?? []), ...queryYears])].filter(y => y > 0).sort((x, y) => y - x);
    return (
        <div ref={group} className="ask-chips" role="group" aria-label="So hat die App die Frage verstanden" tabIndex={-1}>
            {(a.chips ?? []).map(c => {
                const options = choices(c.key);
                const name = CHIP_NAMES[c.key] ?? c.key;
                return (
                    <span key={c.key} className={`chip ask-chip ${c.key === "typ" ? "fixed" : ""}`}>
                        {options ? (
                            <select aria-label={`${name} ändern`} value={current(c.key)}
                                    onChange={e => (c.key === "gruppierung" ? onChange({...q, gruppierung: e.target.value}) : setFilter(c.key, e.target.value))}>
                                {options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
                            </select>
                        ) : c.key === "jahre" && queryYears.length === 1 ? (
                            <select aria-label="Jahr ändern" value={queryYears[0]} onChange={e => onChange({...q, jahre: [Number(e.target.value)]})}>
                                {years.map(y => <option key={y} value={y}>{y}</option>)}
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

// What of a query the invoice list can show: a filter of the list, and the field and value of the query it stands for.
const LIST_FILTERS: [Filter, string, string][] = [
    ["open", "status", "nicht_eingereicht"],
    ["submitted", "status", "eingereicht"],
    ["clarify", "klaerungsbedarf", "ja"],
    ["deadline", "frist_bald", "ja"],
    ["unpaid", "bezahlt", "nein"],
];

/**
 * The invoice list with what of the query it can show: one year (or all),
 * one of its filters, the provider as search (its most telling word – the
 * list searches plain text). missing names the rest.
 */
export function listState(q: QueryDTO): {year: number; state: ListState; missing: string[]} {
    const years = q.jahre ?? [];
    const value = (field: string) => (q.filter ?? []).find(f => f.feld === field)?.wert ?? "";
    const hit = LIST_FILTERS.find(([, field, v]) => value(field) === v);
    // "Dr. Muster" → "Muster": the list's search finds it in "Dr. med. Anna Muster" too.
    const search = value("behandler").split(/[\s.,-]+/).filter(w => w && !/^(dr|med|prof|praxis)$/i.test(w))
        .sort((x, y) => y.length - x.length)[0];
    const state: ListState = {filter: hit?.[0], search: search || undefined};
    const shown = new Set(["behandler", hit?.[1]]);
    const missing = [...new Set((q.filter ?? []).filter(f => !shown.has(f.feld)).map(f => CHIP_NAMES[f.feld] ?? f.feld))];
    if (years.length > 1) missing.push("mehreren Jahren");
    return {year: years.length === 1 ? years[0] : 0, state, missing};
}
