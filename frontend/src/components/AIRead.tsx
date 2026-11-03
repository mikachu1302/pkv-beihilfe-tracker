// The parts of reading a document with the AI that the inbox and the
// invoice form share: the button with progress, and what is known about a field.
import {useEffect, useState} from "react";
import type {ReactNode} from "react";
import type {useAIRead} from "../lib/aiRead";
import {plural} from "../lib/format";
import {alternativeInput, alternativeText} from "../lib/prefill";
import type {Alternative} from "../lib/prefill";
import {SourceBadge} from "./ui";

/**
 * "Mit KI auslesen"; while it runs the same button cancels (so the focus
 * stays on it) and the progress shows, afterwards what it did.
 */
export function AIReadBar({reading, onRead, disabled, done}: {
    reading: ReturnType<typeof useAIRead>;
    onRead: () => void;
    disabled?: boolean;
    done: string;
}) {
    const run = reading.run;
    return (
        <div className="ai-bar">
            <button type="button" onClick={run ? reading.cancel : onRead} disabled={!run && disabled}
                    aria-label={run ? "KI-Auslesen abbrechen" : undefined}
                    title={run ? undefined : "Schickt die Seiten an den KI-Server im eigenen Netz – etwa eine Minute pro Seite"}>
                {run ? "Abbrechen" : "Mit KI auslesen"}
            </button>
            {run ? (
                <span className="muted small">
                    <span role="status">KI liest den Beleg … (etwa {plural(run.pages, "Minute", "Minuten")})</span>{" "}
                    <Elapsed since={run.started}/>
                </span>
            ) : done && <span className="muted small" role="status">{done}</span>}
        </div>
    );
}

/** "1:05" since the start, every second (not read out: the status says what is going on). */
function Elapsed({since}: {since: number}) {
    const [now, setNow] = useState(Date.now());
    useEffect(() => {
        const t = window.setInterval(() => setNow(Date.now()), 1000);
        return () => window.clearInterval(t);
    }, []);
    const s = Math.max(0, Math.floor((now - since) / 1000));
    return <span className="mono" aria-hidden="true">{Math.floor(s / 60)}:{String(s % 60).padStart(2, "0")}</span>;
}

/** Another reading of a field ("KI: 98,50 €") with a button to take it. */
export function AlternativeHint({field, alt, onTake}: {field: string; alt: Alternative; onTake: () => void}) {
    const text = alternativeText(field, alt);
    return (
        <span className="alt-hint">
            {text}{" "}
            <button type="button" className="link" aria-label={`${text} übernehmen`} onClick={onTake}>übernehmen</button>
        </span>
    );
}

/**
 * The hint of a prefilled field: where its value came from (else the usual
 * hint) and another reading of it, which onTake puts into the field (as the
 * form holds it: amounts "286,40").
 */
export function fieldHint(field: string, source: string | undefined, alt: Alternative | undefined,
                          onTake: (value: string) => void, normal?: ReactNode): ReactNode {
    const badge = source ? <SourceBadge source={source}/> : normal;
    if (!alt) return badge;
    return <>{badge}<AlternativeHint field={field} alt={alt} onTake={() => onTake(alternativeInput(field, alt))}/></>;
}
