import {useState} from "react";
import {Desktop, Svc} from "../api";
import {useApp} from "../App";
import {useToast} from "../components/feedback";
import {ErrorNotice, Field} from "../components/ui";
import {errMsg} from "../lib/format";

/** First start: choose the data folder, then set up Beihilfe/PKV. */
export default function Setup() {
    const {status, reloadStatus} = useApp();
    return (
        <main className="content narrow">
            <h1>PKV · Beihilfe einrichten</h1>
            {status.error && <ErrorNotice error={status.error}/>}
            {!status.configured ? <ChooseFolder onDone={reloadStatus}/> : <Coverage onDone={reloadStatus}/>}
        </main>
    );
}

function ChooseFolder({onDone}: {onDone: () => void}) {
    const toast = useToast();
    const [dir, setDir] = useState("");
    const [error, setError] = useState("");

    async function pick() {
        try {
            const d = await Desktop.ChooseFolder("Datenordner wählen");
            if (d) setDir(d);
        } catch (e) {
            toast.error(e);
        }
    }

    async function save() {
        setError("");
        try {
            await Svc.SetDataDir(dir);
            onDone();
        } catch (e) {
            setError(errMsg(e));
        }
    }

    return (
        <div className="panel">
            <h2>1. Datenordner</h2>
            <p>
                Alle Rechnungen und Einstellungen werden als einzelne Dateien in diesem Ordner gespeichert.
                Er kann in einem Cloud-Ordner liegen (Nextcloud, iCloud Drive, Google Drive, OneDrive, Dropbox,
                Syncthing …) – dann sind die Daten auf allen Geräten gleich – oder einfach lokal.
                Ein vorhandener Datenordner wird übernommen.
            </p>
            {error && <ErrorNotice error={error}/>}
            <div className="row">
                <input aria-label="Datenordner" className="grow" value={dir} onChange={e => setDir(e.target.value)}
                       placeholder="Ordner wählen …"/>
                <button onClick={pick}>Durchsuchen …</button>
                <button className="primary" disabled={!dir} onClick={save}>Weiter</button>
            </div>
        </div>
    );
}

function Coverage({onDone}: {onDone: () => void}) {
    const [withBeihilfe, setWithBeihilfe] = useState(true);
    const [beihilfeName, setBeihilfeName] = useState("Beihilfe");
    const [rate, setRate] = useState("50");
    const [bundRules, setBundRules] = useState(false);
    const [pkvName, setPkvName] = useState("");
    const [from, setFrom] = useState(`${new Date().getFullYear() - 3}-01-01`);
    const [error, setError] = useState("");

    async function save() {
        setError("");
        try {
            await Svc.Setup({withBeihilfe, beihilfeName, beihilfeRate: rate, bundRules, pkvName, from});
            onDone();
        } catch (e) {
            setError(errMsg(e));
        }
    }

    const pkvRate = withBeihilfe ? 100 - Number(rate) : 100;
    return (
        <div className="panel">
            <h2>2. Versicherung</h2>
            {error && <ErrorNotice error={error}/>}
            <fieldset>
                <legend>Wie sind Sie versichert?</legend>
                <div className="checks">
                    <label className="inline">
                        <input type="radio" name="mode" checked={withBeihilfe} onChange={() => setWithBeihilfe(true)}/>
                        Beihilfe + private Krankenversicherung
                    </label>
                    <label className="inline">
                        <input type="radio" name="mode" checked={!withBeihilfe} onChange={() => setWithBeihilfe(false)}/>
                        Nur private Krankenversicherung (100 %)
                    </label>
                </div>
            </fieldset>
            {withBeihilfe && (
                <fieldset>
                    <legend>Beihilfe</legend>
                    <div className="grid">
                        <Field label="Name der Beihilfestelle" htmlFor="bh-name">
                            <input id="bh-name" value={beihilfeName} onChange={e => setBeihilfeName(e.target.value)}/>
                        </Field>
                        <Field label="Bemessungssatz" htmlFor="bh-rate" hint="70 % z. B. ab zwei Kindern oder als Versorgungsempfänger">
                            <select id="bh-rate" value={rate} onChange={e => setRate(e.target.value)}>
                                <option value="50">50 %</option>
                                <option value="70">70 %</option>
                            </select>
                        </Field>
                    </div>
                    <label className="inline mt-3">
                        <input type="checkbox" checked={bundRules} onChange={e => setBundRules(e.target.checked)}/>
                        Regeln des Bundes (BBhV): Antrag innerhalb von 3 Jahren, Mindestbetrag 200 €
                    </label>
                    <div className="hint small muted">Für andere Länder lassen sich Frist und Mindestbetrag später in den Einstellungen eintragen.</div>
                </fieldset>
            )}
            <fieldset>
                <legend>Private Krankenversicherung ({pkvRate} %)</legend>
                <div className="grid">
                    <Field label="Name der Versicherung" htmlFor="pkv-name">
                        <input id="pkv-name" value={pkvName} onChange={e => setPkvName(e.target.value)} placeholder="z. B. Muster PKV"/>
                    </Field>
                    <Field label="Gültig ab" htmlFor="from" hint="Ab wann diese Aufteilung gilt; Änderungen später in den Einstellungen">
                        <input id="from" type="date" value={from} onChange={e => setFrom(e.target.value)}/>
                    </Field>
                </div>
            </fieldset>
            <div className="actions">
                <button className="primary" onClick={save}>Fertig</button>
            </div>
        </div>
    );
}
