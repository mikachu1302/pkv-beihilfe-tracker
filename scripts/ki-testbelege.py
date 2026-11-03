#!/usr/bin/env python3
"""Erfundene, aber realistische Testbelege für den KI-Test des Abgleichs (Plan v0.6,
docs/KI-TEST.md) – keine echten Daten.

    python3 scripts/ki-testbelege.py ~/Belegtest-v06
    python3 scripts/ki-belegtest.py test ~/Belegtest-v06

Eine erfundene Patientin (Beihilfe 50 %, PKV 50 %) und ihre Belege eines Herbstes, die
zueinander passen wie im Alltag:

- GOÄ-Rechnung über eine Abrechnungsstelle (2 Seiten, Übertrag, Faktor 3,5 mit Begründung);
- GOZ-Rechnung mit Zahnangaben und beigefügter Laborrechnung (3 Seiten);
- Heil- und Kostenplan zur Krone in der Zahnarztrechnung (mit Abweichungen zur Rechnung);
- Verordnung für Krankengymnastik und die Rechnung der Physiotherapie (eine Behandlung mehr
  als verordnet);
- Beihilfebescheid (2 Seiten: Zeilen mit Hinweisnummern, die Texte auf Seite 2) und
  PKV-Leistungsabrechnung mit Bemerkungen und Selbstbehalt;
- einige davon zusätzlich als schlechte Handyfotos (schief, dunkel, mit Schatten).

Dazu erwartet.json mit Belegart, Kopfdaten und Positionen/Zeilen für ki-belegtest.py.
Namen, Adressen, Nummern und Bankverbindungen sind erfunden (die IBANs haben nur gültige
Prüfziffern). Beträge nach GOÄ/GOZ (Punktwert 5,82873 bzw. 5,62421 Cent). Braucht
ImageMagick (magick).
"""

import json
import subprocess
import sys
import textwrap
from decimal import ROUND_HALF_UP, Decimal
from pathlib import Path

W, H = 1240, 1754  # A4 mit 150 dpi
CENT = Decimal("0.01")
GOAE_PW = Decimal("0.0582873")
GOZ_PW = Decimal("0.0562421")
PATIENT = ["Frau", "Erika Muster", "Lindenweg 3", "54321 Beispielort"]


def cents(d) -> Decimal:
    return Decimal(d).quantize(CENT, ROUND_HALF_UP)


def eur(d) -> str:
    """1234.5 → „1.234,50“"""
    return f"{cents(d):,.2f}".replace(",", "X").replace(".", ",").replace("X", ".")


def fee(points: int, factor: str, pw: Decimal) -> Decimal:
    return cents(Decimal(points) * pw * Decimal(factor.replace(",", ".")))


def iban(blz: str, konto: str) -> str:
    bban = blz + konto.zfill(10)
    return f"DE{98 - int(bban + '131400') % 97:02d}{bban}"


def spaced(i: str) -> str:
    return " ".join(i[k:k + 4] for k in range(0, len(i), 4))


def iso(d: str) -> str:
    day, month, year = d.split(".")
    return f"{year}-{month}-{day}"


class Page:
    """Eine Seite aus Text und Linien, gezeichnet mit ImageMagick."""

    def __init__(self, font="Liberation-Sans", w=W, h=H):
        self.font, self.w, self.h, self.ops = font, w, h, []

    def text(self, x, y, s, size=20, bold=False, right=False, font=None, color="black"):
        f = (font or self.font) + ("-Bold" if bold else "")
        self.ops += ["-font", f, "-fill", color, "-pointsize", str(size), "-gravity", "NorthEast" if right else "NorthWest",
                     "-annotate", f"+{self.w - x if right else x}+{y}", s.replace("%", "%%")]  # % leitet sonst Platzhalter ein

    def para(self, x, y, s, width, size=20, step=None, **kw) -> int:
        """Text mit Zeilenumbruch nach width Zeichen; gibt die nächste freie Zeile zurück."""
        step = step or int(size * 1.4)
        for part in textwrap.wrap(s, width):
            self.text(x, y, part, size, **kw)
            y += step
        return y

    def line(self, x1, y1, x2, y2, width=1, color="#333333"):
        self.ops += ["-stroke", color, "-strokewidth", str(width), "-draw", f"line {x1},{y1} {x2},{y2}", "+stroke"]

    def save(self, path: Path):
        subprocess.run(["magick", "-size", f"{self.w}x{self.h}", "xc:white", *self.ops, str(path)], check=True)


def table(p: Page, y: int, cols, rows, size=19, step=27, x0=90, x1=W - 90) -> int:
    """cols: (Titel, x, rechtsbündig, Umbruch nach Zeichen oder 0). Titel mit \\n zweizeilig."""
    head = max(c[0].count("\n") + 1 for c in cols)
    for title, x, right, _ in cols:
        for i, part in enumerate(title.split("\n")):
            p.text(x, y + i * (step - 4), part, size - 1, bold=True, right=right)
    y += head * (step - 4) + 6
    p.line(x0, y, x1, y)
    y += 8
    for row in rows:
        height = 1
        for (_, x, right, wrap), cell in zip(cols, row):
            parts = textwrap.wrap(cell, wrap) if wrap and cell else [cell]
            for i, part in enumerate(parts):
                p.text(x, y + i * step, part, size, right=right)
            height = max(height, len(parts))
        y += height * step + 4
    p.line(x0, y, x1, y)
    return y + 10


def letter(p: Page, sender: str, info: list[tuple[str, str]], y=250):
    """Absenderzeile, Anschrift der Patientin und Infoblock rechts."""
    p.text(90, y - 32, sender, 13)
    p.line(90, y - 12, 560, y - 12, color="#888888")
    for i, s in enumerate(PATIENT):
        p.text(90, y + i * 30, s, 22)
    for i, (label, value) in enumerate(info):
        p.text(720, y + i * 28, label, 17)
        p.text(1150, y + i * 28, value, 17, right=True)


def footer(p: Page, lines: list[str], page: int, pages: int):
    p.line(90, 1640, 1150, 1640, color="#888888")
    for i, s in enumerate(lines):
        p.text(90, 1652 + i * 20, s, 13)
    p.text(1150, 1700, f"Seite {page} von {pages}", 14, right=True)


# --- die Belege ----------------------------------------------------------------------------------

def goae_invoice(out: Path) -> tuple[dict, dict]:
    """Arztrechnung über eine Abrechnungsstelle, 2 Seiten."""
    catalogue = {"1": (80, "Beratung – auch mittels Fernsprecher"),
                 "3": (150, "Eingehende, das gewöhnliche Maß übersteigende Beratung – auch mittels Fernsprecher"),
                 "5": (80, "Symptombezogene Untersuchung"),
                 "7": (160, "Vollständige körperliche Untersuchung mindestens eines Organsystems"),
                 "34": (300, "Erörterung (Dauer mindestens 20 Minuten) der Auswirkungen einer Krankheit auf die Lebensgestaltung"),
                 "75": (130, "Ausführlicher schriftlicher Krankheits- und Befundbericht"),
                 "250": (40, "Blutentnahme mittels Spritze, Kanüle oder Katheter aus der Vene"),
                 "410": (200, "Ultraschalluntersuchung eines Organs"),
                 "420": (80, "Ultraschalluntersuchung von bis zu drei weiteren Organen im Anschluss an Nr. 410, je Organ"),
                 "651": (253, "Elektrokardiographische Untersuchung in Ruhe"),
                 "3550": (60, "Blutbild und Blutbildbestandteile"),
                 "3560": (40, "Glukose"),
                 "3561": (200, "Glykierte Hämoglobine (HbA1, HbA1c)"),
                 "3585.H1": (40, "Kreatinin")}
    plan = [("08.09.2026", "1", 1, "2,3"), ("08.09.2026", "7", 1, "2,3"), ("08.09.2026", "250", 1, "1,8"),
            ("08.09.2026", "3550", 1, "1,15"), ("08.09.2026", "3560", 1, "1,15"), ("08.09.2026", "3561", 1, "1,15"),
            ("08.09.2026", "3585.H1", 1, "1,15"), ("15.09.2026", "3", 1, "3,5"), ("15.09.2026", "651", 1, "1,8"),
            ("15.09.2026", "410", 1, "1,8"), ("15.09.2026", "420", 2, "1,8"), ("22.09.2026", "1", 1, "2,3"),
            ("22.09.2026", "5", 1, "2,3"), ("22.09.2026", "34", 1, "2,3"), ("29.09.2026", "1", 1, "2,3"),
            ("29.09.2026", "75", 1, "2,3")]
    rows, positions = [], []
    for day, nr, n, factor in plan:
        points, text = catalogue[nr]
        amount = fee(points, factor, GOAE_PW) * n
        rows.append((day, nr, text, str(n), factor, eur(amount)))
        positions.append({"datum": iso(day), "ziffer": nr, "text": text, "anzahl": str(n), "betrag": str(amount)})
    total = sum(Decimal(p["betrag"]) for p in positions)
    cut = fee(150, "3,5", GOAE_PW) - fee(150, "2,3", GOAE_PW)  # the Bescheid cuts Nr. 3 to 2,3
    pvs_iban = iban("12030000", "4455667788")
    cols = [("Datum", 90, False, 0), ("GOÄ-Nr.", 225, False, 0), ("Leistung", 330, False, 40), ("Anz.", 820, True, 0),
            ("Faktor", 920, True, 0), ("Betrag\nEUR", 1150, True, 0)]
    foot = ["Muster-PVS GmbH · Amtsgericht Musterstadt HRB 1234 · Geschäftsführung: Max Beispiel",
            f"Bankverbindung: Musterbank · IBAN {spaced(pvs_iban)} · BIC TESTDEFFXXX"]

    d = out / "rechnung-goae"
    d.mkdir(exist_ok=True)
    p = Page()
    p.text(1150, 70, "Muster-PVS GmbH", 30, bold=True, right=True)
    p.text(1150, 110, "Privatärztliche Abrechnungsstelle", 18, right=True)
    p.text(1150, 136, "Postfach 10 20 30 · 12345 Musterstadt · Telefon 01234 567890", 15, right=True)
    letter(p, "Muster-PVS GmbH · Postfach 10 20 30 · 12345 Musterstadt",
           [("Rechnungsnummer", "R-2026-1187"), ("Rechnungsdatum", "12.10.2026"), ("Kundennummer", "7712-04"),
            ("Bitte bei Zahlung angeben", "")])
    p.text(90, 420, "Rechnung für ärztliche Leistungen", 28, bold=True)
    y = p.para(90, 470, "für die Behandlung durch Dr. med. Hans Probe, Facharzt für Innere Medizin, Hauptstraße 12, "
                         "12345 Musterstadt", 95, 19)
    y = p.para(90, y, "Patientin: Erika Muster, geb. 14.03.1971 · Behandlungszeitraum: 08.09.2026 – 29.09.2026", 95, 19)
    y = p.para(90, y, "Diagnosen: Essentielle Hypertonie (I10.90); Diabetes mellitus Typ 2 ohne Komplikationen (E11.90)", 95, 19)
    y = table(p, y + 20, cols, rows[:10])
    carry = sum(Decimal(x["betrag"]) for x in positions[:10])
    p.text(800, y, "Übertrag", 19, bold=True)
    p.text(1150, y, eur(carry), 19, bold=True, right=True)
    footer(p, foot, 1, 2)
    p.save(d / "01.png")

    p = Page()
    p.text(90, 80, "Rechnung R-2026-1187 vom 12.10.2026 · Patientin Erika Muster · Dr. med. Hans Probe", 16)
    p.text(800, 130, "Übertrag", 19, bold=True)
    p.text(1150, 130, eur(carry), 19, bold=True, right=True)
    y = table(p, 175, cols, rows[10:])
    p.text(760, y + 10, "Rechnungsbetrag", 22, bold=True)
    p.text(1150, y + 10, eur(total) + " EUR", 22, bold=True, right=True)
    y = p.para(90, y + 80, "Begründung gemäß § 12 Abs. 3 GOÄ: Nr. 3 am 15.09.2026, Faktor 3,5 – deutlich erhöhter "
                            "Zeitaufwand (45 Minuten) bei Neueinstellung der Medikation und Erläuterung der "
                            "Wechselwirkungen.", 90, 18)
    y = p.para(90, y + 20, "Bitte überweisen Sie den Rechnungsbetrag bis zum 11.11.2026 unter Angabe der "
                            "Rechnungsnummer auf das Konto der Muster-PVS GmbH:", 90, 19)
    p.text(90, y + 4, f"IBAN {spaced(pvs_iban)} · Musterbank", 19, bold=True)
    p.para(90, y + 60, "Die Rechnung wird im Auftrag und für Rechnung des behandelnden Arztes erstellt. "
                       "Bei Fragen zur Rechnung erreichen Sie uns montags bis freitags von 8 bis 16 Uhr.", 90, 17)
    footer(p, foot, 2, 2)
    p.save(d / "02.png")

    expected = {"art": "rechnung", "behandler": "Hans Probe", "zahlungsempfaenger": "Muster-PVS GmbH",
                "rechnungsdatum": "2026-10-12", "behandlung_von": "2026-09-08", "behandlung_bis": "2026-09-29",
                "betrag": str(total), "auslagen": "0", "rechnungsnummer": "R-2026-1187", "iban": pvs_iban,
                "positionen": positions}
    return expected, {"total": total, "cut": cut}


GOZ = {"0050": (120, "Abformung oder Teilabformung eines Kiefers für ein Situationsmodell"),
       "0090": (60, "Intraorale Infiltrationsanästhesie"),
       "1040": (28, "Professionelle Zahnreinigung, je Zahn"),
       "2030": (65, "Besondere Maßnahmen beim Präparieren oder Füllen von Kavitäten"),
       "2197": (130, "Adhäsive Befestigung (plastischer Aufbau, Stift, Inlay, Krone, Teilkrone, Veneer)"),
       "2210": (1678, "Versorgung eines Zahnes durch eine Vollkrone (Hohlkehl- oder Stufenpräparation)"),
       "2270": (270, "Provisorium im direkten Verfahren mit Abformung, je Zahn"),
       "Ä1": (80, "Beratung – auch mittels Fernsprecher"),
       "Ä925a": (200, "Röntgendiagnostik der Zähne, bis zwei Einzelaufnahmen")}


def goz_rows(plan):
    rows, positions = [], []
    for day, tooth, nr, n, factor in plan:
        points, text = GOZ[nr]
        amount = fee(points, factor, GOAE_PW if nr.startswith("Ä") else GOZ_PW) * n
        rows.append(((day,) if day else ()) + (tooth, nr, text, str(n), factor, eur(amount)))
        positions.append({**({"datum": iso(day)} if day else {}), "zahn": tooth, "ziffer": nr, "text": text,
                          "anzahl": str(n), "betrag": str(amount)})
    return rows, positions


DENTIST = "Zahnarztpraxis Dr. Ruth Beispiel"


def dentist_head(p: Page):
    p.text(90, 60, DENTIST, 30, bold=True, font="Liberation-Serif")
    p.text(90, 102, "Zahnärztin · Implantologie · Prophylaxe", 18, font="Liberation-Serif")
    p.text(1150, 66, "Am Markt 7", 17, right=True)
    p.text(1150, 90, "12345 Musterstadt", 17, right=True)
    p.text(1150, 114, "Telefon 01234 98765", 17, right=True)


def goz_invoice(out: Path) -> tuple[dict, dict]:
    """Zahnarztrechnung mit Zahnangaben, Laborkosten und beigefügter Laborrechnung, 3 Seiten."""
    plan = [("06.10.2026", "36", "0090", 1, "2,3"), ("06.10.2026", "36", "2210", 1, "2,3"),
            ("06.10.2026", "36", "2270", 1, "2,3"), ("06.10.2026", "OK/UK", "0050", 2, "2,3"),
            ("20.10.2026", "36", "0090", 1, "2,3"), ("20.10.2026", "36", "2197", 1, "2,3"),
            ("20.10.2026", "36", "2030", 1, "2,3"), ("27.10.2026", "", "Ä1", 1, "2,3"),
            ("27.10.2026", "", "1040", 28, "2,3")]
    rows, positions = goz_rows(plan)
    fees = sum(Decimal(p["betrag"]) for p in positions)
    lab_items = [("0010", "Modell aus Superhartgips", 2, Decimal("14.50")), ("0120", "Einzelstumpf sägen und vorbereiten", 1, Decimal("18.40")),
                 ("1022", "Vollkrone Zirkonoxid, monolithisch", 1, Decimal("365.00")), ("8002", "Zirkonoxid-Rohling, anteilig", 1, Decimal("42.00")),
                 ("9330", "Versandkosten", 1, Decimal("8.90"))]
    net = sum(n * price for _, _, n, price in lab_items)
    vat = cents(net * Decimal("0.07"))
    lab = net + vat
    total = fees + lab
    own_iban = iban("25010030", "9182736450")
    cols = [("Datum", 90, False, 0), ("Zahn", 210, False, 0), ("Nr.", 290, False, 0), ("Leistung", 380, False, 36),
            ("Anz.", 820, True, 0), ("Faktor", 920, True, 0), ("Betrag\nEUR", 1150, True, 0)]
    foot = [f"{DENTIST} · Am Markt 7 · 12345 Musterstadt · Steuernummer 12/345/67890",
            f"Sparkasse Musterstadt · IBAN {spaced(own_iban)} · BIC TESTDEMMXXX"]

    d = out / "rechnung-goz"
    d.mkdir(exist_ok=True)
    p = Page(font="Liberation-Sans")
    dentist_head(p)
    letter(p, f"{DENTIST} · Am Markt 7 · 12345 Musterstadt",
           [("Rechnung Nr.", "ZA-2026-0412"), ("Datum", "03.11.2026"), ("Patientin", "Erika Muster"), ("geb.", "14.03.1971")])
    p.text(90, 420, "Liquidation", 28, bold=True)
    y = p.para(90, 465, "für zahnärztliche Leistungen nach der Gebührenordnung für Zahnärzte (GOZ) und Ärzte (GOÄ) "
                         "im Zeitraum 06.10.2026 bis 27.10.2026", 95, 19)
    y = table(p, y + 20, cols, rows[:6])
    carry = sum(Decimal(x["betrag"]) for x in positions[:6])
    p.text(800, y, "Übertrag", 19, bold=True)
    p.text(1150, y, eur(carry), 19, bold=True, right=True)
    footer(p, foot, 1, 3)
    p.save(d / "01.png")

    p = Page()
    p.text(90, 80, "Rechnung ZA-2026-0412 vom 03.11.2026 · Erika Muster", 16)
    p.text(800, 130, "Übertrag", 19, bold=True)
    p.text(1150, 130, eur(carry), 19, bold=True, right=True)
    y = table(p, 175, cols, rows[6:])
    sums = [("Zahnärztliches Honorar", eur(fees)), ("Material- und Laborkosten (§ 9 GOZ)", eur(lab)),
            ("gemäß beigefügter Laborrechnung", "")]
    for i, (label, value) in enumerate(sums):
        p.text(560, y + 10 + i * 30, label, 19)
        p.text(1150, y + 10 + i * 30, value, 19, right=True)
    y += 10 + len(sums) * 30
    p.line(560, y, 1150, y)
    p.text(560, y + 12, "Rechnungsbetrag", 23, bold=True)
    p.text(1150, y + 12, eur(total) + " €", 23, bold=True, right=True)
    y = p.para(90, y + 90, "Bitte überweisen Sie den Betrag innerhalb von 30 Tagen nach Erhalt der Rechnung auf unser "
                            f"Konto bei der Sparkasse Musterstadt, IBAN {spaced(own_iban)}.", 90, 19)
    p.para(90, y + 20, "Zahnärztliche Leistungen sind nach § 4 Nr. 14 UStG umsatzsteuerfrei.", 90, 17)
    footer(p, foot, 2, 3)
    p.save(d / "02.png")

    p = Page(font="Carlito")
    p.text(90, 70, "Dentallabor Musterzahn GmbH", 28, bold=True)
    p.text(90, 108, "Laborweg 2 · 12345 Musterstadt · Meisterbetrieb", 17)
    p.text(90, 200, f"Auftraggeber: {DENTIST}, Am Markt 7, 12345 Musterstadt", 18)
    p.text(90, 228, "Patientin: Erika Muster · Arbeit: Vollkrone Zahn 36 · Auftrag 26-3381", 18)
    p.text(90, 290, "Laborrechnung Nr. L-26-3381 vom 28.10.2026", 24, bold=True)
    lab_rows = [(nr, text, str(n), eur(price), eur(n * price)) for nr, text, n, price in lab_items]
    y = table(p, 350, [("BEB-Nr.", 90, False, 0), ("Leistung / Material", 220, False, 45), ("Menge", 760, True, 0),
                       ("Einzelpreis", 920, True, 0), ("Gesamt", 1150, True, 0)], lab_rows)
    for i, (label, value) in enumerate([("Summe netto", eur(net)), ("zzgl. 7 % MwSt.", eur(vat))]):
        p.text(700, y + 10 + i * 30, label, 19)
        p.text(1150, y + 10 + i * 30, value, 19, right=True)
    p.text(700, y + 80, "Gesamtbetrag", 22, bold=True)
    p.text(1150, y + 80, eur(lab) + " €", 22, bold=True, right=True)
    p.para(90, y + 150, "Herstellungsland: Deutschland. Die Konformitätserklärung nach MDR liegt der Arbeit bei. "
                        "Die Laborrechnung wird mit der Rechnung der Zahnarztpraxis abgerechnet – bitte nicht gesondert "
                        "überweisen.", 90, 17)
    footer(p, ["Dentallabor Musterzahn GmbH · HRB 5678 · USt-IdNr. DE000000000"], 3, 3)
    p.save(d / "03.png")

    expected = {"art": "rechnung", "behandler": "Ruth Beispiel", "rechnungsdatum": "2026-11-03",
                "behandlung_von": "2026-10-06", "behandlung_bis": "2026-10-27", "betrag": str(total),
                "auslagen": str(lab), "rechnungsnummer": "ZA-2026-0412", "iban": own_iban, "positionen": positions}
    return expected, {"total": total, "fees": fees, "lab": lab}


def cost_plan(out: Path) -> dict:
    """Heil- und Kostenplan zur Krone 36 (die Rechnung weicht ab: Röntgen fehlt, 2030 kommt dazu)."""
    plan = [("", "36", "Ä925a", 1, "1,8"), ("", "36", "0090", 2, "2,3"), ("", "36", "2210", 1, "2,3"),
            ("", "36", "2270", 1, "2,3"), ("", "OK/UK", "0050", 2, "2,3"), ("", "36", "2197", 1, "2,3")]
    rows, positions = goz_rows(plan)
    fees = sum(Decimal(p["betrag"]) for p in positions)
    lab = Decimal("450.00")
    p = Page()
    dentist_head(p)
    letter(p, f"{DENTIST} · Am Markt 7 · 12345 Musterstadt",
           [("Datum", "15.09.2026"), ("Patientin", "Erika Muster"), ("geb.", "14.03.1971")])
    p.text(90, 420, "Heil- und Kostenplan / Kostenvoranschlag", 28, bold=True)
    y = p.para(90, 470, "Geplante Behandlung: Versorgung des Zahnes 36 mit einer Vollkrone aus Zirkonoxid. "
                         "Befund: 36 tief zerstört, wurzelbehandelt, nicht mehr füllbar.", 95, 19)
    y = table(p, y + 20, [("Zahn", 90, False, 0), ("Nr.", 180, False, 0), ("Leistung", 290, False, 44),
                          ("Anz.", 820, True, 0), ("Faktor", 920, True, 0), ("Betrag\nEUR", 1150, True, 0)], rows)
    sums = [("Zahnärztliches Honorar (voraussichtlich)", eur(fees)),
            ("Material- und Laborkosten (geschätzt)", eur(lab))]
    for i, (label, value) in enumerate(sums):
        p.text(520, y + 10 + i * 30, label, 19)
        p.text(1150, y + 10 + i * 30, value, 19, right=True)
    y += 10 + len(sums) * 30
    p.line(520, y, 1150, y)
    p.text(520, y + 12, "Gesamtkosten (voraussichtlich)", 22, bold=True)
    p.text(1150, y + 12, eur(fees + lab) + " €", 22, bold=True, right=True)
    y = p.para(90, y + 90, "Dieser Kostenplan ist unverbindlich. Die tatsächlichen Kosten können je nach Behandlungs"
                            "verlauf abweichen; Material- und Laborkosten sind geschätzt. Wie viel Ihre Beihilfe und Ihre "
                            "private Krankenversicherung erstatten, hängt von Ihrem Tarif ab – bitte reichen Sie den "
                            "Kostenplan vor Behandlungsbeginn dort ein.", 92, 18)
    p.para(90, y + 30, "Der Kostenplan ist sechs Monate gültig.", 92, 18)
    p.text(90, y + 120, "Musterstadt, 15.09.2026", 19)
    p.text(700, y + 110, "R. Beispiel", 34, font="C059-Italic", color="#1a2a6a")
    p.line(700, y + 160, 1100, y + 160)
    p.text(700, y + 168, "Dr. Ruth Beispiel", 16)
    footer(p, [f"{DENTIST} · Am Markt 7 · 12345 Musterstadt"], 1, 1)
    p.save(out / "kostenplan-zahn.png")
    return {"art": "kostenplan", "behandler": "Ruth Beispiel", "datum": "2026-09-15", "honorar": str(fees),
            "material_labor": str(lab), "gesamt": str(fees + lab), "eigenanteil": "0", "positionen": positions}


def prescription(out: Path) -> dict:
    """Privatrezept für Krankengymnastik und Fango, A5 quer."""
    p = Page(w=1240, h=874)
    p.text(90, 50, "Dr. med. Klara Beispiel", 28, bold=True)
    p.text(90, 88, "Fachärztin für Orthopädie und Unfallchirurgie", 17)
    p.text(90, 112, "Bahnhofstraße 5 · 12345 Musterstadt · Telefon 01234 24680", 17)
    p.text(1150, 50, "Privatrezept", 30, bold=True, right=True, color="#003a70")
    p.text(1150, 92, "Datum: 10.09.2026", 19, right=True)
    p.line(90, 145, 1150, 145, 2, color="#003a70")
    p.text(90, 165, "Für: Erika Muster, Lindenweg 3, 54321 Beispielort · geb. 14.03.1971", 19)
    p.text(90, 220, "Verordnung von Heilmitteln", 22, bold=True)
    p.text(120, 262, "6 × Krankengymnastik (Einzelbehandlung)", 21)
    p.text(120, 296, "6 × Wärmetherapie mittels Fango-Packung", 21)
    p.text(120, 340, "Frequenz: 2 × wöchentlich", 20)
    p.text(120, 374, "Diagnose: Lumbalgie, chronisch rezidivierend (ICD-10: M54.5)", 20)
    p.text(120, 408, "Therapieziel: Schmerzlinderung, Verbesserung der Beweglichkeit und Rumpfstabilität", 20)
    p.text(120, 452, "Behandlungsbeginn spätestens am: 08.10.2026", 20, bold=True)
    p.text(120, 486, "Hausbesuch: nein · Therapiebericht: ja", 18)
    p.text(760, 640, "K. Beispiel", 40, font="C059-Italic", color="#1a2a6a")
    p.line(720, 700, 1120, 700)
    p.text(720, 708, "Unterschrift und Stempel der Ärztin", 15)
    p.ops += ["-stroke", "#7a1f2b", "-strokewidth", "2", "-fill", "none", "-draw", "roundrectangle 90,600 520,780 12,12", "+stroke"]
    p.text(110, 618, "Dr. med. Klara Beispiel", 18, color="#7a1f2b")
    p.text(110, 646, "Orthopädie", 16, color="#7a1f2b")
    p.text(110, 672, "Bahnhofstraße 5, 12345 Musterstadt", 16, color="#7a1f2b")
    p.save(out / "verordnung-physio.png")
    return {"art": "verordnung", "verordner": "Klara Beispiel", "datum": "2026-09-10",
            "heilmittel": [{"heilmittel": "Krankengymnastik", "anzahl": "6"}, {"heilmittel": "Fango", "anzahl": "6"}],
            "frequenz": "2 x wöchentlich", "diagnose": "Lumbalgie", "icd": "M54.5", "gueltig_bis": "2026-10-08"}


def physio_invoice(out: Path) -> tuple[dict, dict]:
    """Rechnung der Physiotherapie: 7 × KG (verordnet waren 6), 6 × Fango."""
    days = ["22.09.2026", "25.09.2026", "29.09.2026", "02.10.2026", "06.10.2026", "09.10.2026", "13.10.2026"]
    kg, fango = Decimal("32.00"), Decimal("14.50")
    rows, positions = [], []
    for i, day in enumerate(days):
        items = [("KG", "Krankengymnastik, Einzelbehandlung", kg)] + ([("WP", "Wärmepackung (Fango)", fango)] if i < 6 else [])
        for code, text, price in items:
            rows.append((day, code, text, "1", eur(price), eur(price)))
            positions.append({"datum": iso(day), "ziffer": code, "text": text, "anzahl": "1", "betrag": str(price)})
    total = sum(Decimal(x["betrag"]) for x in positions)
    own_iban = iban("37050198", "1234509876")
    p = Page(font="Carlito")
    p.text(90, 60, "Physiotherapie Lotte Probe", 32, bold=True)
    p.text(90, 104, "Praxis für Physiotherapie und Manuelle Therapie", 18)
    letter(p, "Physiotherapie Lotte Probe · Gartenstraße 4 · 12345 Musterstadt",
           [("Rechnungsnummer", "PT-2026-091"), ("Rechnungsdatum", "23.10.2026"), ("Patientin", "Erika Muster")])
    p.text(90, 420, "Rechnung", 28, bold=True)
    y = p.para(90, 465, "Gemäß ärztlicher Verordnung vom 10.09.2026 (Dr. med. Klara Beispiel) berechnen wir Ihnen "
                         "folgende Leistungen:", 95, 19)
    y = table(p, y + 15, [("Datum", 90, False, 0), ("Pos.", 230, False, 0), ("Leistung", 320, False, 40),
                          ("Anz.", 800, True, 0), ("Einzelpreis", 960, True, 0), ("Betrag", 1150, True, 0)], rows, step=26)
    p.text(760, y + 10, "Gesamtbetrag", 22, bold=True)
    p.text(1150, y + 10, eur(total) + " €", 22, bold=True, right=True)
    y = p.para(90, y + 80, "Heilmittelleistungen sind nach § 4 Nr. 14 UStG von der Umsatzsteuer befreit. Bitte "
                            f"überweisen Sie den Betrag bis zum 22.11.2026 auf das Konto IBAN {spaced(own_iban)}.", 92, 18)
    footer(p, ["Physiotherapie Lotte Probe · Gartenstraße 4 · 12345 Musterstadt · IK 123456789"], 1, 1)
    p.save(out / "rechnung-physio.png")
    expected = {"art": "rechnung", "behandler": "Lotte Probe", "rechnungsdatum": "2026-10-23",
                "behandlung_von": "2026-09-22", "behandlung_bis": "2026-10-13", "betrag": str(total), "auslagen": "0",
                "rechnungsnummer": "PT-2026-091", "iban": own_iban, "positionen": positions}
    return expected, {"total": total, "kg": len(days), "fango": 6}


def beihilfe_notice(out: Path, goae: dict, goz: dict, physio: dict) -> dict:
    """Beihilfebescheid, 2 Seiten: Zeilen mit Hinweisnummern, die Hinweise auf Seite 2."""
    pharmacy = Decimal("23.92")
    lines = [("12.10.2026", "Dr. med. Hans Probe (über Muster-PVS GmbH)", goae["total"], goae["total"] - goae["cut"], "1"),
             ("21.10.2026", "Rosen-Apotheke, Arzneimittel", pharmacy, pharmacy - Decimal("5.00"), "2"),
             ("23.10.2026", "Physiotherapie Lotte Probe, Heilmittel", physio["total"],
              6 * Decimal("28.20") + 6 * Decimal("13.10"), "3, 4"),
             ("03.11.2026", "Zahnarztpraxis Dr. Ruth Beispiel", goz["total"], goz["fees"] + cents(goz["lab"] * Decimal("0.6")), "5")]
    notes = [("1", "Der Gebührensatz über dem 2,3-fachen (Nr. 3 GOÄ am 15.09.2026) ist nicht ausreichend begründet. "
                   f"Beihilfefähig ist der 2,3-fache Satz; nicht beihilfefähig: {eur(goae['cut'])} €."),
             ("2", "Nicht verschreibungspflichtige Arzneimittel sind nicht beihilfefähig (§ 22 BVO); nicht beihilfefähig: 5,00 €."),
             ("3", "Aufwendungen für Heilmittel sind nur bis zu den Höchstbeträgen der Anlage 9 zur BVO beihilfefähig "
                   "(Krankengymnastik 28,20 €, Wärmepackung 13,10 € je Behandlung)."),
             ("4", "Verordnet waren 6 Behandlungen Krankengymnastik; die 7. Behandlung vom 13.10.2026 ist nicht beihilfefähig."),
             ("5", "Aufwendungen für zahntechnische Leistungen (Material- und Laborkosten) sind zu 60 % beihilfefähig (§ 16 BVO).")]
    rows, zeilen = [], []
    for i, (day, who, billed, eligible, hint) in enumerate(lines, 1):
        refund = cents(eligible * Decimal("0.5"))
        rows.append((str(i), day, who, eur(billed), eur(eligible), "50 %", eur(refund), hint))
        zeilen.append({"rechnungsdatum": iso(day), "behandler": who.split(",")[0].split(" (")[0], "rechnungsbetrag": str(billed),
                       "erstattungsfaehig": str(eligible), "satz": "50", "erstattung": str(refund), "hinweis_nr": hint})
    total = sum(Decimal(z["erstattung"]) for z in zeilen)
    deduction = Decimal("100.00")
    payout = total - deduction
    foot = ["Beihilfestelle Musterland · Landesamt für Finanzen (Beispiel) · Beihilfestraße 1 · 12340 Musterstadt",
            "Sprechzeiten: Mo–Do 9–12 Uhr · Telefon 01234 1111-0 · www.beihilfe.example"]

    d = out / "bescheid-beihilfe"
    d.mkdir(exist_ok=True)
    p = Page(font="Carlito")
    p.text(90, 60, "Beihilfestelle Musterland", 28, bold=True)
    p.text(90, 98, "Landesamt für Finanzen (Beispiel)", 19)
    p.text(90, 124, "Beihilfestraße 1 · 12340 Musterstadt", 17)
    letter(p, "Landesamt für Finanzen · Beihilfestelle · 12340 Musterstadt",
           [("Aktenzeichen", "BH 4711-0815"), ("Datum", "18.11.2026"), ("Sachbearbeitung", "Team 3"),
            ("Telefon", "01234 1111-30")])
    p.text(90, 420, "Beihilfebescheid", 30, bold=True)
    p.text(90, 466, "Ihr Antrag vom 04.11.2026, eingegangen am 06.11.2026", 20)
    y = p.para(90, 510, "Sehr geehrte Frau Muster, auf Ihren Antrag wird Ihnen folgende Beihilfe gewährt:", 95, 20)
    cols = [("Nr.", 90, False, 0), ("Beleg vom", 135, False, 0), ("Leistungserbringer / Art", 265, False, 24),
            ("Rechnungs-\nbetrag", 770, True, 0), ("beihilfe-\nfähig", 900, True, 0), ("Satz", 975, True, 0),
            ("Beihilfe", 1090, True, 0), ("Hinw.", 1102, False, 0)]
    y = table(p, y + 15, cols, rows)
    for i, (label, value, bold) in enumerate([("Summe Beihilfe", eur(total), False),
                                              ("abzüglich Kostendämpfungspauschale 2026", "– " + eur(deduction), False),
                                              ("Auszahlungsbetrag", eur(payout) + " €", True)]):
        p.text(520, y + 10 + i * 32, label, 21 if bold else 19, bold=bold)
        p.text(1090, y + 10 + i * 32, value, 21 if bold else 19, bold=bold, right=True)
    y = p.para(90, y + 140, "Der Auszahlungsbetrag wird in den nächsten Tagen auf Ihr Konto IBAN "
                             f"{spaced(iban('54321000', '1112223334'))} überwiesen.", 95, 19)
    p.para(90, y + 10, "Die Erläuterungen zu den Hinweisen (Spalte „Hinw.“) finden Sie auf Seite 2.", 95, 19)
    footer(p, foot, 1, 2)
    p.save(d / "01.png")

    p = Page(font="Carlito")
    p.text(90, 70, "Beihilfebescheid vom 18.11.2026 · Aktenzeichen BH 4711-0815", 16)
    p.text(90, 130, "Hinweise", 24, bold=True)
    y = 175
    for nr, text in notes:
        p.text(90, y, f"({nr})", 19)
        y = p.para(140, y, text, 88, 19) + 14
    p.text(90, y + 20, "Rechtsbehelfsbelehrung", 22, bold=True)
    y = p.para(90, y + 60, "Gegen diesen Bescheid kann innerhalb eines Monats nach Bekanntgabe Widerspruch erhoben "
                            "werden. Der Widerspruch ist schriftlich oder zur Niederschrift bei der oben genannten "
                            "Beihilfestelle einzulegen.", 95, 18)
    p.text(90, y + 30, "Mit freundlichen Grüßen", 19)
    p.text(90, y + 60, "Ihre Beihilfestelle", 19)
    p.text(90, y + 110, "Dieser Bescheid wurde maschinell erstellt und ist ohne Unterschrift gültig.", 16)
    footer(p, foot, 2, 2)
    p.save(d / "02.png")
    return {"art": "bescheid", "kostentraeger": "Beihilfestelle Musterland", "bescheiddatum": "2026-11-18",
            "antrag_vom": "2026-11-04", "auszahlung": str(payout),
            "abzuege": [{"text": "Kostendämpfungspauschale 2026", "betrag": str(deduction)}],
            "zeilen": zeilen, "hinweise": [{"nr": nr, "text": text} for nr, text in notes]}


def pkv_statement(out: Path, goae: dict, goz: dict, physio: dict) -> dict:
    """PKV-Leistungsabrechnung, 1 Seite, Bemerkungen mit Kürzeln, Selbstbehalt."""
    pharmacy = Decimal("23.92")
    lines = [("12.10.2026", "Dr. med. Hans Probe", "BT50", goae["total"], goae["total"], ""),
             ("21.10.2026", "Rosen-Apotheke", "BT50", pharmacy, pharmacy - Decimal("5.00"), "A3"),
             ("23.10.2026", "Physiotherapie L. Probe", "BT50", physio["total"], physio["kg"] * Decimal("30.00") + 6 * Decimal("14.50"), "H1"),
             ("03.11.2026", "Zahnarztpraxis Dr. Beispiel", "ZT50", goz["total"], goz["total"], "")]
    notes = [("A3", "Nicht verschreibungspflichtige Arzneimittel sind im Tarif BT50 nicht versichert."),
             ("H1", "Heilmittel erstatten wir bis zu den Höchstbeträgen des Tarifs (Krankengymnastik 30,00 € je Behandlung).")]
    rows, zeilen = [], []
    for day, who, tariff, billed, eligible, hint in lines:
        refund = cents(eligible * Decimal("0.5"))
        rows.append((day, who, tariff, eur(billed), eur(eligible), "50", eur(refund), hint))
        zeilen.append({"rechnungsdatum": iso(day), "behandler": who, "rechnungsbetrag": str(billed),
                       "erstattungsfaehig": str(eligible), "satz": "50", "erstattung": str(refund), "hinweis_nr": hint})
    total = sum(Decimal(z["erstattung"]) for z in zeilen)
    deduction = Decimal("75.00")
    payout = total - deduction
    p = Page()
    p.text(90, 60, "Muster Krankenversicherung a. G.", 28, bold=True, color="#0b4f3c")
    p.text(90, 98, "Leistungsabteilung · Versicherungsallee 10 · 50000 Musterstadt", 17)
    letter(p, "Muster Krankenversicherung a. G. · 50000 Musterstadt",
           [("Versicherungsnummer", "123 456 789-0"), ("Datum", "20.11.2026"), ("Versicherte Person", "Erika Muster")])
    p.text(90, 420, "Leistungsabrechnung", 28, bold=True)
    p.text(90, 464, "Ihre Einreichung vom 04.11.2026 – wir haben die Rechnungen wie folgt abgerechnet:", 19)
    y = table(p, 510, [("Rechnung\nvom", 90, False, 0), ("Leistungserbringer", 225, False, 22), ("Tarif", 520, False, 0),
                       ("Rechnungs-\nbetrag", 720, True, 0), ("erstattungs-\nfähig", 860, True, 0), ("%", 905, True, 0),
                       ("Erstattung", 1060, True, 0), ("Bem.", 1078, False, 0)], rows, size=18)
    for i, (label, value, bold) in enumerate([("Summe Erstattung", eur(total), False),
                                              ("abzüglich Selbstbehalt Tarif BT50 (Kalenderjahr 2026)", "– " + eur(deduction), False),
                                              ("Auszahlungsbetrag", eur(payout) + " €", True)]):
        p.text(440, y + 10 + i * 32, label, 21 if bold else 19, bold=bold)
        p.text(1060, y + 10 + i * 32, value, 21 if bold else 19, bold=bold, right=True)
    y += 140
    p.text(90, y, "Bemerkungen", 20, bold=True)
    y += 34
    for code, text in notes:
        p.text(90, y, code, 18, bold=True)
        y = p.para(150, y, text, 90, 18) + 8
    p.para(90, y + 30, f"Den Auszahlungsbetrag überweisen wir auf Ihr Konto IBAN {spaced(iban('54321000', '1112223334'))}. "
                       "Bitte bewahren Sie die Rechnungen auf – wir können sie bis zu drei Jahre später anfordern.", 92, 18)
    footer(p, ["Muster Krankenversicherung a. G. · Vorstand: Erika Beispiel (Vors.) · Sitz Musterstadt"], 1, 1)
    p.save(out / "abrechnung-pkv.png")
    return {"art": "bescheid", "kostentraeger": "Muster Krankenversicherung", "bescheiddatum": "2026-11-20",
            "antrag_vom": "2026-11-04", "auszahlung": str(payout),
            "abzuege": [{"text": "Selbstbehalt", "betrag": str(deduction)}],
            "zeilen": zeilen, "hinweise": [{"nr": c, "text": t} for c, t in notes]}


# --- Handyfotos ----------------------------------------------------------------------------------

def photo(src: Path, dst: Path, style: str):
    """Macht aus einer sauberen Seite ein schlechtes Handyfoto."""
    if style == "schief":  # schräg fotografiert, Schatten von oben links, leicht unscharf
        fx = ["-background", "#cfc8bc", "-rotate", "3.5",
              "-distort", "Perspective", "0,0 50,70  1300,0 1220,10  0,1850 10,1850  1300,1850 1300,1800",
              "(", "-size", "1400x1950", "gradient:#ffffff-#857d70", "-rotate", "35", ")", "-gravity", "center",
              "-compose", "multiply", "-composite", "-blur", "0x1.1", "-attenuate", "0.5", "+noise", "Gaussian",
              "-resize", "1200x", "-quality", "68"]
    elif style == "dunkel":  # Kunstlicht am Abend, wenig Kontrast, gelbstichig
        fx = ["-background", "#888070", "-rotate", "-1.5", "+level", "25%,64%", "-modulate", "100,40", "-fill", "#6a5a3a",
              "-colorize", "18", "-blur", "0x1.3", "-attenuate", "0.7", "+noise", "Gaussian", "-resize", "1100x", "-quality", "62"]
    elif style == "knick":  # gefaltet: dunkler Streifen über die Mitte, schräg
        w, h = map(int, subprocess.run(["magick", "identify", "-format", "%w %h", str(src)], check=True,
                                       capture_output=True, text=True).stdout.split())
        fx = ["(", "+clone", "-fill", "white", "-colorize", "100", "-fill", "#a39d90", "-draw",
              f"rectangle 0,{h // 2 - 6} {w},{h // 2 + 6}", "-blur", "0x30", ")", "-compose", "multiply", "-composite",
              "-background", "#d0cabe", "-rotate", "-2.5",
              "-blur", "0x0.9", "-attenuate", "0.5", "+noise", "Gaussian", "-resize", "1150x", "-quality", "66"]
    elif style == "tisch":  # kleines Rezept auf dem Holztisch, gedreht
        fx = ["-bordercolor", "white", "-border", "10", "-background", "#6b4a2f", "-rotate", "7", "-gravity", "center",
              "-extent", "1500x1200", "(", "-size", "1500x1200", "gradient:#ffffff-#6d675c", "-rotate", "20", ")",
              "-compose", "multiply", "-composite", "-blur", "0x1.0", "-attenuate", "0.6", "+noise", "Gaussian",
              "-resize", "1300x", "-quality", "66"]
    else:
        raise ValueError(style)
    subprocess.run(["magick", str(src), *fx, str(dst)], check=True)


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    out = Path(sys.argv[1]).expanduser()
    out.mkdir(parents=True, exist_ok=True)
    expected = {}
    expected["rechnung-goae"], goae = goae_invoice(out)
    expected["rechnung-goz"], goz = goz_invoice(out)
    expected["kostenplan-zahn.png"] = cost_plan(out)
    expected["verordnung-physio.png"] = prescription(out)
    expected["rechnung-physio.png"], physio = physio_invoice(out)
    expected["bescheid-beihilfe"] = beihilfe_notice(out, goae, goz, physio)
    expected["abrechnung-pkv.png"] = pkv_statement(out, goae, goz, physio)

    photos = [("foto-rechnung-goae", "rechnung-goae", [("01.png", "schief"), ("02.png", "dunkel")]),
              ("foto-bescheid-beihilfe", "bescheid-beihilfe", [("01.png", "knick"), ("02.png", "schief")]),
              ("foto-kostenplan-zahn.jpg", "kostenplan-zahn.png", [("", "dunkel")]),
              ("foto-verordnung-physio.jpg", "verordnung-physio.png", [("", "tisch")]),
              ("foto-abrechnung-pkv.jpg", "abrechnung-pkv.png", [("", "knick")])]
    for name, clean, pages in photos:
        if pages[0][0]:
            (out / name).mkdir(exist_ok=True)
            for page, style in pages:
                photo(out / clean / page, out / name / page.replace(".png", ".jpg"), style)
        else:
            photo(out / clean, out / name, pages[0][1])
        expected[name] = expected[clean]

    (out / "erwartet.json").write_text(json.dumps({"dokumente": expected}, ensure_ascii=False, indent=2) + "\n")
    print(f"{out}: {len(expected)} Belege, erwartete Werte in erwartet.json")


if __name__ == "__main__":
    main()
