import type {PayerYearDTO} from "../api";
import {eur, num} from "../lib/format";

/**
 * What each payer still owes: SOLL − IST of the claims that are submitted or
 * still to be submitted (calc.Outstanding). Held-back SOLL is listed, not counted.
 */
export function OutstandingCards({payers, period}: {payers: PayerYearDTO[]; period: string}) {
    const shown = payers.filter(p => p.claims > 0);
    if (shown.length === 0) return null;
    return (
        <div className="cards outstanding">
            {shown.map(p => {
                const o = p.outstanding;
                const parts = [
                    num(o.submitted) !== 0 && `eingereicht, offen ${eur(o.submitted)}`,
                    num(o.open) !== 0 && `noch nicht eingereicht ${eur(o.open)}`,
                    num(o.refunded) !== 0 && `Abweichung bei erstatteten ${eur(o.refunded)}`,
                ].filter(Boolean);
                return (
                    <div key={p.payerId} className="card">
                        <div className="label">{p.payerName} · Ausstand {period}</div>
                        <div className="value">{eur(o.total)}</div>
                        <div className="note">SOLL {eur(o.expected)} − IST {eur(o.actual)}</div>
                        {parts.length > 0 && <div className="note">davon {parts.join(" · ")}</div>}
                        {num(o.held) !== 0 && <div className="note">zurückgehalten {eur(o.held)} – nicht eingerechnet</div>}
                    </div>
                );
            })}
        </div>
    );
}
