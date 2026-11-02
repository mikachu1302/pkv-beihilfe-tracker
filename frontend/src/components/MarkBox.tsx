import {useState} from "react";
import type {MarkDTO} from "../api";
import {date, shortDate} from "../lib/format";

/**
 * A checkbox that remembers the day it was ticked. Ticking records today
 * (done by the backend); the date next to the box can be clicked to change it.
 */
export function MarkBox({label, mark, onToggle, onDate, disabled}: {
    label: string;
    mark: MarkDTO;
    onToggle: (on: boolean) => void;
    onDate?: (date: string) => void;
    disabled?: boolean;
}) {
    const [editing, setEditing] = useState(false);
    const done = mark.done;
    return (
        <span className="markbox">
            <label className="mark-check">
                <input type="checkbox" aria-label={label} checked={done} disabled={disabled}
                       onChange={e => onToggle(e.target.checked)}/>
                <span className="mark-label">{label}</span>
            </label>
            {done && !editing && (
                onDate
                    ? <button type="button" className="mark-date" title={`${label} am ${date(mark.date)} – Datum ändern`}
                              aria-label={`${label}: Datum ${date(mark.date)} ändern`}
                              onClick={() => setEditing(true)}>{shortDate(mark.date)}</button>
                    : <span>{shortDate(mark.date)}</span>
            )}
            {done && editing && (
                <input type="date" autoFocus defaultValue={mark.date} aria-label={`${label}: Datum`}
                       onBlur={e => {
                           setEditing(false);
                           const v = e.currentTarget.value;
                           if (v && v !== mark.date) onDate?.(v);
                       }}
                       onKeyDown={e => {
                           if (e.key === "Enter") e.currentTarget.blur();
                           if (e.key === "Escape") setEditing(false);
                       }}/>
            )}
        </span>
    );
}
