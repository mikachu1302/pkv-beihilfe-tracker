// Einstellungen · KI: the connection to the local AI server (this device only).
import {useCallback, useEffect, useState} from "react";
import {Svc} from "../api";
import type {AISettingsDTO, AITestDTO} from "../api";
import {useToast} from "../components/feedback";
import {ErrorNotice, Field} from "../components/ui";
import {setAI, useAI} from "../lib/ai";
import {errMsg} from "../lib/format";
import {useUnsavedChanges} from "../lib/shortcuts";
import {useBackend} from "../lib/useBackend";
import {Cancelled, useCancellable} from "../lib/useCancellable";

/** Value of the option "Anderes Modell eintragen …" (no model name can be it). */
const OTHER = "\u0000other";

export function AISettingsSection() {
    const toast = useToast();
    const stored = useAI();
    // Normally loaded at start (App); if that failed, here again – with a message if it fails once more.
    const load = useBackend(() => (stored ? Promise.resolve(stored) : Svc.AISettings()), []);
    useEffect(() => {
        if (!stored && load.data) setAI(load.data);
    }, [stored, load.data]);
    const loaded = stored ?? load.data ?? null;
    // Address and model as typed; saved when leaving the field, with Enter or "Übernehmen" (and before a test).
    const [url, setUrl] = useState("");
    const [model, setModel] = useState("");
    const [key, setKey] = useState("");
    const [changingKey, setChangingKey] = useState(false);
    const [result, setResult] = useState<AITestDTO | null>(null);
    const [testError, setTestError] = useState("");
    const test = useCancellable();
    // The server's models the app can use, for the choice; null = no list (the name is typed).
    const [models, setModels] = useState<string[] | null>(null);
    const [typing, setTyping] = useState(false); // "Anderes Modell eintragen …" chosen
    const [backToList, setBackToList] = useState(false); // focus the choice when it comes back
    const {run: list} = useCancellable();
    useEffect(() => {
        if (!loaded) return;
        setUrl(loaded.url);
        setModel(loaded.model);
        // Only when another address or model was loaded, not after switching on or off.
    }, [loaded?.url, loaded?.model]); // eslint-disable-line react-hooks/exhaustive-deps

    const loadModels = useCallback(async () => {
        try {
            const ids = await list(() => Svc.AIModels());
            // Someone typing a name keeps the field.
            const f = document.activeElement;
            if (f instanceof HTMLInputElement && f.id === "ai-model") setTyping(true);
            setModels(ids);
        } catch (e) {
            if (!(e instanceof Cancelled)) setModels(null);
        }
    }, [list]);
    // Asked when the page is open with the AI on, again after another address or key, and after
    // each test (below) – the request carries nothing but the key.
    useEffect(() => {
        if (loaded?.on) void loadModels();
    }, [loaded?.on, loaded?.url, loaded?.keySet, loaded?.keyEnd, loadModels]);

    const edited = loaded !== null && (url.trim() !== loaded.url || model.trim() !== loaded.model);
    useUnsavedChanges(edited || key.trim() !== "");
    if (!loaded) {
        if (!load.error) return null;
        return (
            <section className="panel" aria-label="KI">
                <h2>KI</h2>
                <ErrorNotice error={`Die KI-Einstellungen konnten nicht geladen werden: ${load.error}`}/>
                <button onClick={load.reload}>Erneut laden</button>
            </section>
        );
    }
    const keyForm = !loaded.keySet || changingKey;

    /** Saves the settings; null (and a message) when that failed. */
    async function apply(next: AISettingsDTO): Promise<AISettingsDTO | null> {
        try {
            const a = await Svc.SetAISettings(next);
            setAI(a);
            return a;
        } catch (e) {
            toast.error(e);
            return null;
        }
    }

    /** Saves address and model as typed or chosen (the backend tidies them up). */
    async function saveServer(m = model): Promise<boolean> {
        const a = await apply({...loaded!, url, model: m});
        if (a) {
            setUrl(a.url);
            setModel(a.model);
        }
        return a !== null;
    }
    const saveEdited = () => {
        if (edited) void saveServer();
    };

    function choose(value: string) {
        if (value === OTHER) {
            setTyping(true);
            return;
        }
        setModel(value);
        void saveServer(value);
    }

    async function saveKey(value: string) {
        try {
            setAI(await Svc.SetAIKey(value));
            setKey("");
            setChangingKey(false);
            setResult(null);
            toast.show(value ? "Schlüssel gespeichert" : "Schlüssel entfernt");
        } catch (e) {
            toast.error(e);
        }
    }

    async function runTest() {
        setResult(null);
        setTestError("");
        if (edited && !await saveServer()) return;
        try {
            setResult(await test.run(() => Svc.TestAI()));
        } catch (e) {
            if (!(e instanceof Cancelled)) setTestError(errMsg(e));
            return;
        }
        void loadModels();
    }
    const choices = !typing && models && models.length > 0 ? models : null;

    return (
        <section className="panel" aria-label="KI">
            <h2>KI</h2>
            <p className="muted small">
                Ein KI-Modell auf einem Rechner im eigenen Netz liest auf Wunsch schwierige Belege und beantwortet Fragen
                zu den eigenen Daten – keine Cloud. Es läuft nur, wenn man es anstößt; ein Beleg geht dann unverschlüsselt
                durchs eigene WLAN zum Server. Gilt nur für dieses Gerät.
            </p>
            <label className="inline">
                <input type="checkbox" checked={loaded.on} onChange={e => void apply({...loaded, on: e.target.checked})}/>
                KI-Funktionen verwenden
            </label>
            <div className="grid mt-2">
                <Field label="Serveradresse" htmlFor="ai-url" hint="OpenAI-kompatibel (z. B. llama.cpp), endet auf /v1">
                    <input id="ai-url" value={url} spellCheck={false} onChange={e => setUrl(e.target.value)} onBlur={saveEdited}
                           onKeyDown={e => e.key === "Enter" && saveEdited()}/>
                </Field>
                <Field label="Modell" htmlFor="ai-model"
                       hint={choices ? "Die Modelle des Servers, die Bilder lesen" : "Ein Modell, das auch Bilder liest – eines nur für Text nimmt die App nicht"}>
                    {choices ? (
                        <select id="ai-model" value={model} autoFocus={backToList} onChange={e => choose(e.target.value)}>
                            {!choices.includes(model) && <option value={model}>{model || "(keines)"} – nicht in der Liste</option>}
                            {choices.map(m => <option key={m} value={m}>{m}</option>)}
                            <option value={OTHER}>Anderes Modell eintragen …</option>
                        </select>
                    ) : (
                        <input id="ai-model" value={model} spellCheck={false} autoFocus={typing} onChange={e => setModel(e.target.value)}
                               onBlur={saveEdited} onKeyDown={e => e.key === "Enter" && saveEdited()}/>
                    )}
                    {typing && models && models.length > 0 && <button className="link" onClick={() => {
                        setTyping(false);
                        setBackToList(true);
                    }}>Aus der Liste wählen</button>}
                </Field>
                <Field label="API-Schlüssel" htmlFor={keyForm ? "ai-key" : undefined}
                       hint={keyForm ? "Wird nur auf diesem Gerät gespeichert und nicht wieder angezeigt" : undefined}>
                    {keyForm ? (
                        <span className="row nowrap">
                            <input id="ai-key" type="password" autoComplete="off" spellCheck={false} value={key} autoFocus={changingKey}
                                   onChange={e => setKey(e.target.value)}
                                   onKeyDown={e => e.key === "Enter" && key.trim() && void saveKey(key)}/>
                            <button disabled={!key.trim()} onClick={() => void saveKey(key)}>Schlüssel speichern</button>
                            {loaded.keySet && <button className="link" aria-label="Schlüssel ändern abbrechen" onClick={() => {
                                setKey("");
                                setChangingKey(false);
                            }}>Abbrechen</button>}
                        </span>
                    ) : (
                        <span className="row nowrap">
                            <span className="key-stored">gespeichert{loaded.keyEnd && <span className="mono"> (…{loaded.keyEnd})</span>}</span>
                            <button className="link" onClick={() => setChangingKey(true)}>Ändern</button>
                            <button className="link" onClick={() => void saveKey("")}>Entfernen</button>
                        </span>
                    )}
                </Field>
            </div>
            <div className="actions">
                {edited && <button className="primary" onClick={() => void saveServer().then(ok => ok && toast.show("KI-Server übernommen"))}>Übernehmen</button>}
                {/* One button that cancels while the test runs, so the focus stays on it. */}
                <button onClick={test.running ? test.cancel : () => void runTest()} aria-label={test.running ? "Verbindungstest abbrechen" : undefined}>
                    {test.running ? "Abbrechen" : "Verbindung testen"}
                </button>
            </div>
            <div role="status">
                {test.running && <p className="muted small">Verbindung wird getestet …</p>}
                {testError && <p className="neg small">{testError}</p>}
                {result && (
                    <ul className="ai-checks">
                        {(result.checks ?? []).map(c => (
                            <li key={c.text} className={c.ok ? "pos" : "neg"}>
                                <span aria-hidden="true">{c.ok ? "✓" : "✗"}</span> {c.text}
                            </li>
                        ))}
                        {result.ok && !loaded.on && <li className="muted">Zum Verwenden oben „KI-Funktionen verwenden“ einschalten.</li>}
                    </ul>
                )}
            </div>
        </section>
    );
}
