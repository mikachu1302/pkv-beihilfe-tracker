// Einstellungen · KI: the connection to the local AI server (this device only).
import {useEffect, useRef, useState} from "react";
import {Svc} from "../api";
import type {AISettingsDTO, AITestDTO} from "../api";
import {useToast} from "../components/feedback";
import {Field} from "../components/ui";
import {setAI, useAI} from "../lib/ai";
import {errMsg} from "../lib/format";
import {useUnsavedChanges} from "../lib/shortcuts";

export function AISettingsSection() {
    const toast = useToast();
    const loaded = useAI();
    // Address and model as typed; saved with "Übernehmen" (or before a test).
    const [url, setUrl] = useState("");
    const [model, setModel] = useState("");
    const [key, setKey] = useState("");
    const [changingKey, setChangingKey] = useState(false);
    const [busy, setBusy] = useState(false);
    const [result, setResult] = useState<AITestDTO | null>(null);
    const [testError, setTestError] = useState("");
    const test = useRef<{cancel: () => void} | null>(null);
    useEffect(() => () => test.current?.cancel(), []);
    useEffect(() => {
        if (!loaded) return;
        setUrl(loaded.url);
        setModel(loaded.model);
        // Only when another address or model was loaded, not after switching on or off.
    }, [loaded?.url, loaded?.model]); // eslint-disable-line react-hooks/exhaustive-deps

    const edited = loaded !== null && (url.trim() !== loaded.url || model.trim() !== loaded.model);
    useUnsavedChanges(edited || key.trim() !== "");
    if (!loaded) return null;
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
        const p = Svc.TestAI();
        const run = {cancel: () => p.cancel()};
        test.current = run;
        setBusy(true);
        try {
            const r = await p;
            if (test.current === run) setResult(r);
        } catch (e) {
            if (test.current === run) setTestError(errMsg(e));
        } finally {
            if (test.current === run) {
                test.current = null;
                setBusy(false);
            }
        }
    }

    function cancelTest() {
        const t = test.current;
        test.current = null;
        t?.cancel();
        setBusy(false);
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
                    <input id="ai-url" value={url} spellCheck={false} onChange={e => setUrl(e.target.value)}/>
                </Field>
                <Field label="Modell" htmlFor="ai-model" hint="Ein Modell, das auch Bilder liest">
                    <input id="ai-model" value={model} spellCheck={false} onChange={e => setModel(e.target.value)}/>
                </Field>
                <Field label="API-Schlüssel" htmlFor={keyForm ? "ai-key" : undefined}
                       hint={keyForm ? "Wird nur auf diesem Gerät gespeichert und nicht wieder angezeigt" : undefined}>
                    {keyForm ? (
                        <span className="row nowrap">
                            <input id="ai-key" type="password" autoComplete="off" spellCheck={false} value={key}
                                   onChange={e => setKey(e.target.value)}
                                   onKeyDown={e => e.key === "Enter" && key.trim() && void saveKey(key)}/>
                            <button disabled={!key.trim()} onClick={() => void saveKey(key)}>Speichern</button>
                            {loaded.keySet && <button className="link" onClick={() => {
                                setKey("");
                                setChangingKey(false);
                            }}>Abbrechen</button>}
                        </span>
                    ) : (
                        <span className="row nowrap">
                            <span>gespeichert{loaded.keyEnd && <span className="mono"> (…{loaded.keyEnd})</span>}</span>
                            <button className="link" onClick={() => setChangingKey(true)}>Ändern</button>
                            <button className="link" onClick={() => void saveKey("")}>Entfernen</button>
                        </span>
                    )}
                </Field>
            </div>
            <div className="actions">
                {edited && <button className="primary" onClick={() => void saveServer().then(ok => ok && toast.show("KI-Server übernommen"))}>Übernehmen</button>}
                <button disabled={busy} onClick={() => void runTest()}>{busy ? "Verbindung wird getestet …" : "Verbindung testen"}</button>
                {busy && <button className="link" onClick={cancelTest}>Abbrechen</button>}
            </div>
            <div role="status">
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
