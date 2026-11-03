// Einstellungen · KI: the connection to the local AI server (this device only).
import {useEffect, useState} from "react";
import {Svc} from "../api";
import type {AISettingsDTO, AITestDTO} from "../api";
import {useToast} from "../components/feedback";
import {ErrorNotice, Field} from "../components/ui";
import {setAI, useAI} from "../lib/ai";
import {errMsg} from "../lib/format";
import {useUnsavedChanges} from "../lib/shortcuts";
import {useBackend} from "../lib/useBackend";
import {Cancelled, useCancellable} from "../lib/useCancellable";

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
    useEffect(() => {
        if (!loaded) return;
        setUrl(loaded.url);
        setModel(loaded.model);
        // Only when another address or model was loaded, not after switching on or off.
    }, [loaded?.url, loaded?.model]); // eslint-disable-line react-hooks/exhaustive-deps

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

    /** Saves address and model as typed (the backend tidies them up). */
    async function saveServer(): Promise<boolean> {
        const a = await apply({...loaded!, url, model});
        if (a) {
            setUrl(a.url);
            setModel(a.model);
        }
        return a !== null;
    }
    const saveEdited = () => {
        if (edited) void saveServer();
    };

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
        }
    }

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
                <Field label="Modell" htmlFor="ai-model" hint="Ein Modell, das auch Bilder liest">
                    <input id="ai-model" value={model} spellCheck={false} onChange={e => setModel(e.target.value)} onBlur={saveEdited}
                           onKeyDown={e => e.key === "Enter" && saveEdited()}/>
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
