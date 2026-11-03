// Small presentational components shared by the pages.
import {useId} from "react";
import type {ReactNode} from "react";
import {SOURCE} from "../lib/prefill";

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

/** Where a prefilled value came from (see SOURCE), or nothing. */
export function SourceBadge({source}: {source?: string}) {
    if (!source) return null;
    const [label, kind] = SOURCE[source] ?? [source, undefined];
    return <Badge kind={kind}>{label}</Badge>;
}

/**
 * Year dropdown used on several pages. Year 0 holds the invoices without a
 * treatment date ("ohne Datum") – except with allLabel, where 0 means all
 * years (the backend's ListInvoices(0)); undated invoices are included there.
 */
export function YearSelect({years, value, onChange, allLabel}: {
    years: number[];
    value: number;
    onChange: (year: number) => void;
    allLabel?: string;
}) {
    return (
        <select aria-label="Jahr" style={{width: "auto"}} value={value} onChange={e => onChange(Number(e.target.value))}>
            {allLabel && <option value={0}>{allLabel}</option>}
            {years.filter(y => !allLabel || y !== 0).map(y => <option key={y} value={y}>{y === 0 ? "ohne Datum" : y}</option>)}
        </select>
    );
}

/** Labelled form field wrapper. Without htmlFor the label names the group of controls inside. */
export function Field({label, htmlFor, hint, wide, children}: {
    label: string;
    htmlFor?: string;
    hint?: ReactNode;
    wide?: boolean;
    children: ReactNode;
}) {
    const id = useId();
    const group = htmlFor ? {} : {role: "group", "aria-labelledby": id};
    return (
        <div className={`field ${wide ? "wide" : ""}`} {...group}>
            <label id={id} htmlFor={htmlFor}>{label}</label>
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
