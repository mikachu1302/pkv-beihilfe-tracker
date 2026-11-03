// The parts of reading a document with the AI that the inbox and the
// invoice form share: the button with progress, and the other reading of a field.
import type {useAIRead} from "../lib/aiRead";
import {alternativeText} from "../lib/prefill";
import type {Alternative} from "../lib/prefill";

/** "Mit KI auslesen", while running the progress and "Abbrechen", afterwards what it did. */
export function AIReadBar({reading, onRead, disabled, done}: {
    reading: ReturnType<typeof useAIRead>;
    onRead: () => void;
    disabled?: boolean;
    done: string;
}) {
    return (
        <div className="ai-bar">
            {reading.running ? (
                <>
                    <span className="muted small" role="status">{reading.status}</span>
                    <button type="button" className="link" onClick={reading.cancel}>Abbrechen</button>
                </>
            ) : (
                <>
                    <button type="button" onClick={onRead} disabled={disabled}
                            title="Schickt die Seiten an den KI-Server im eigenen Netz – etwa eine Minute pro Seite">
                        Mit KI auslesen
                    </button>
                    {done && <span className="muted small" role="status">{done}</span>}
                </>
            )}
        </div>
    );
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
