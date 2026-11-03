// Small hand-made charts (no chart library): a thin segmented bar and
// horizontal stacked bars. Colours come from the --series-N tokens; values
// are always also shown in a table next to the chart.
import {useState} from "react";
import type {FocusEvent, MouseEvent, ReactNode} from "react";
import {eur, eurRound} from "../lib/format";

interface Tip {
    x: number;
    y: number;
    value: string;
    label: string;
}

/** Tooltip state for chart marks: value first, label below. */
export function useTooltip() {
    const [tip, setTip] = useState<Tip | null>(null);
    const node: ReactNode = tip && (
        <div className="tooltip" role="tooltip" style={{left: tip.x + 14, top: tip.y + 14}}>
            <strong>{tip.value}</strong>
            <span>{tip.label}</span>
        </div>
    );
    const handlers = (value: string, label: string) => ({
        onMouseMove: (e: MouseEvent) => setTip({x: e.clientX, y: e.clientY, value, label}),
        onMouseLeave: () => setTip(null),
        onFocus: (e: FocusEvent<HTMLElement>) => {
            const r = e.currentTarget.getBoundingClientRect();
            setTip({x: r.left, y: r.bottom, value, label});
        },
        onBlur: () => setTip(null),
    });
    return {node, handlers};
}

export interface Segment {
    key: string;
    label: string;
    value: number;
    slot: number; // 1..5 → --series-N
}

function share(v: number, total: number): string {
    return `${Math.round((v / total) * 100).toLocaleString("de-DE")} %`;
}

/** Thin segmented bar: one whole split into parts (no text on the bar). */
export function SegmentBar({segments, label}: {segments: Segment[]; label: string}) {
    const {node, handlers} = useTooltip();
    const total = segments.reduce((s, x) => s + Math.max(x.value, 0), 0);
    if (total <= 0) return null;
    return (
        <>
            <div className="segbar" role="group" aria-label={label}>
                {segments.filter(s => s.value > 0).map(s => (
                    <span key={s.key} className={`seg s${s.slot}`} style={{flexGrow: s.value, flexBasis: 0}} tabIndex={0}
                          aria-label={`${s.label}: ${eur(s.value)} (${share(s.value, total)})`}
                          {...handlers(eur(s.value), `${s.label} · ${share(s.value, total)}`)}/>
                ))}
            </div>
            {node}
        </>
    );
}

export interface StackRow {
    key: string;
    label: string;
    total: number;
    parts: Segment[];
}

/** Horizontal stacked bars on one shared scale, total at the tip. */
export function StackedBars({rows, legend, label}: {rows: StackRow[]; legend: {label: string; slot: number}[]; label: string}) {
    const {node, handlers} = useTooltip();
    const max = Math.max(1, ...rows.map(r => r.parts.reduce((s, p) => s + Math.max(p.value, 0), 0)));
    return (
        <figure className="chart" aria-label={label}>
            <div className="legend">
                {legend.map(l => <span key={l.label}><span className={`swatch s${l.slot}`}/>{l.label}</span>)}
            </div>
            <div className="hbars">
                {rows.map(r => (
                    <div key={r.key} className="contents">
                        <div className="ylabel">{r.label}</div>
                        <div className="track">
                            {r.parts.filter(p => p.value > 0).map(p => (
                                <span key={p.key} className={`part s${p.slot}`} style={{width: `${(p.value / max) * 100}%`}}
                                      tabIndex={0} aria-label={`${r.label}, ${p.label}: ${eur(p.value)}`}
                                      {...handlers(eur(p.value), `${r.label} · ${p.label}`)}/>
                            ))}
                        </div>
                        <div className="tip">{eurRound(r.total)}</div>
                    </div>
                ))}
            </div>
            {node}
        </figure>
    );
}
