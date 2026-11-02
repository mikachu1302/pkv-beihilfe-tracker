// Small presentational components shared by the pages.
import type {ReactNode} from "react";

export function Notice({kind, children}: {kind?: "warn" | "bad" | "good"; children: ReactNode}) {
    return <div className={`notice ${kind ?? ""}`}>{children}</div>;
}

export function Loading() {
    return <div className="empty">Lädt …</div>;
}

export function ErrorNotice({error}: {error: string}) {
    return <Notice kind="bad"><span style={{whiteSpace: "pre-line"}}>{error}</span></Notice>;
}

export function Empty({children}: {children: ReactNode}) {
    return <div className="panel empty">{children}</div>;
}

export function Badge({kind, children, title}: {kind?: "good" | "warn" | "bad"; children: ReactNode; title?: string}) {
    return <span className={`badge ${kind ?? ""}`} title={title}>{children}</span>;
}

/** Year dropdown used on several pages. */
export function YearSelect({years, value, onChange, allLabel}: {
    years: number[];
    value: number;
    onChange: (year: number) => void;
    allLabel?: string;
}) {
    return (
        <select aria-label="Jahr" style={{width: "auto"}} value={value} onChange={e => onChange(Number(e.target.value))}>
            {allLabel && <option value={0}>{allLabel}</option>}
            {years.map(y => <option key={y} value={y}>{y === 0 ? "ohne Datum" : y}</option>)}
        </select>
    );
}

/** Labelled form field wrapper. */
export function Field({label, htmlFor, hint, wide, children}: {
    label: string;
    htmlFor?: string;
    hint?: ReactNode;
    wide?: boolean;
    children: ReactNode;
}) {
    return (
        <div className={`field ${wide ? "wide" : ""}`}>
            <label htmlFor={htmlFor}>{label}</label>
            {children}
            {hint && <div className="hint">{hint}</div>}
        </div>
    );
}

/** Stat tile: label, value, optional note below. */
export function Stat({label, value, note, tone}: {label: string; value: ReactNode; note?: ReactNode; tone?: "pos" | "neg"}) {
    return (
        <div className="card">
            <div className="label">{label}</div>
            <div className={`value ${tone ?? ""}`}>{value}</div>
            {note && <div className="note">{note}</div>}
        </div>
    );
}
