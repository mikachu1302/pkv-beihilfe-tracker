package service

import (
	"slices"
	"strings"
	"unicode"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/xlsximport"
)

// Importing a spreadsheet again: every row is matched to an invoice that
// already exists (by where it came from and by provider, dates and amount).
// A matched row only changes the fields that come from the spreadsheet, and
// with a recorded import source only those that changed in the file since
// the last import – edits made in the app stay.

// Row states in the preview.
const (
	rowNew       = "new"
	rowChanged   = "changed"
	rowUnchanged = "unchanged"
	rowDeleted   = "deleted" // the invoice was deleted in the app
)

// excelFields returns the spreadsheet fields of an invoice as comparable
// strings (see model.ImportSource.Values). payers are the payers the
// spreadsheet columns belong to.
func excelFields(inv *model.Invoice, payers []string) map[string]string {
	f := map[string]string{
		"provider":    strings.TrimSpace(inv.Provider),
		"invoiceDate": inv.InvoiceDate.String(),
		"serviceFrom": inv.ServiceFrom.String(),
		"amount":      inv.Amount.String(),
		"paid":        "",
		"note":        strings.TrimSpace(inv.Note),
	}
	if inv.Paid.Done() {
		f["paid"] = inv.Paid.Date.String()
	}
	for _, p := range payers {
		c := inv.Claims[p]
		f["submitted:"+p] = ""
		if c.Submitted.Done() {
			f["submitted:"+p] = c.Submitted.Date.String()
		}
		var sum money.Cents
		found := false
		for _, r := range c.Refunds {
			if fromSpreadsheet(r) {
				sum += r.Amount
				found = true
			}
		}
		f["refund:"+p] = ""
		if found {
			f["refund:"+p] = sum.String()
		}
	}
	return f
}

func fromSpreadsheet(r model.Refund) bool { return r.DocID == "" && r.Note == xlsximport.RefundNote }

// fieldOrder is the order in which changes are listed.
var fieldOrder = []string{"provider", "invoiceDate", "serviceFrom", "amount", "paid", "note"}

func fieldKeys(payers []string) []string {
	keys := slices.Clone(fieldOrder)
	for _, p := range payers {
		keys = append(keys, "submitted:"+p, "refund:"+p)
	}
	return keys
}

// change is one field the import would set.
type change struct {
	key, from, to string
	// conflict: the app has its own value (changed since the last import,
	// or unknown because there is no record of it): the row needs a look.
	conflict bool
}

// changes returns what importing row (spreadsheet values) into an invoice
// with the current values cur would change. base are the values of the last
// import (nil if unknown): fields equal to base were not corrected in the
// file and keep their value from the app. Without a base the file can only
// fill empty fields – it never clears a value set in the app – and other
// differences are conflicts. ownRefund names payers with refunds booked in
// the app (by hand or from a Bescheid): a refund from the file would count
// twice, so it is not taken over.
func changes(row, cur, base map[string]string, keys []string, ownRefund map[string]bool) []change {
	var out []change
	for _, k := range keys {
		to, from := row[k], cur[k]
		if to == from {
			continue
		}
		if p, ok := strings.CutPrefix(k, "refund:"); ok && ownRefund[p] {
			continue
		}
		if base == nil {
			if to == "" {
				continue // an empty cell does not undo what was entered in the app
			}
			out = append(out, change{key: k, from: from, to: to, conflict: from != ""})
			continue
		}
		if to == base[k] { // a missing key (payer not mapped last time) counts as empty
			continue // changed only in the app
		}
		out = append(out, change{key: k, from: from, to: to, conflict: cur[k] != base[k]})
	}
	return out
}

// ownRefunds returns the payers with refunds that did not come from the spreadsheet.
func ownRefunds(inv *model.Invoice) map[string]bool {
	own := map[string]bool{}
	for p, c := range inv.Claims {
		for _, r := range c.Refunds {
			if !fromSpreadsheet(r) {
				own[p] = true
			}
		}
	}
	return own
}

// rowChanges is changes for a spreadsheet row and its matched invoice.
func rowChanges(row map[string]string, inv *model.Invoice, payers []string) []change {
	var base map[string]string
	if inv.Source != nil {
		base = inv.Source.Values
	}
	return changes(row, excelFields(inv, payers), base, fieldKeys(payers), ownRefunds(inv))
}

// applyField sets one spreadsheet field of inv.
func applyField(inv *model.Invoice, key, value string, newID func() string) {
	date := func() civil.Date { d, _ := civil.Parse(value); return d }
	switch key {
	case "provider":
		inv.Provider = value
	case "invoiceDate":
		inv.InvoiceDate = date()
	case "serviceFrom":
		inv.ServiceFrom = date()
	case "amount":
		inv.Amount, _ = money.Parse(value)
	case "paid":
		if value == "" {
			inv.Paid = model.Mark{}
		} else {
			inv.Paid = model.Mark{Date: date(), Note: inv.Paid.Note}
		}
	case "note":
		inv.Note = value
	default:
		kind, payer, ok := strings.Cut(key, ":")
		if !ok {
			return
		}
		if inv.Claims == nil {
			inv.Claims = map[string]model.Claim{}
		}
		c := inv.Claims[payer]
		switch kind {
		case "submitted":
			if value == "" {
				c.Submitted, c.SubmissionID = model.Mark{}, ""
			} else {
				c.Submitted = model.Mark{Date: date(), Note: c.Submitted.Note}
			}
		case "refund":
			c.Refunds = slices.DeleteFunc(slices.Clone(c.Refunds), fromSpreadsheet)
			if v, err := money.Parse(value); err == nil && value != "" {
				c.Refunds = append(c.Refunds, model.Refund{ID: newID(), Amount: v, Note: xlsximport.RefundNote})
			}
		}
		if c.IsZero() {
			delete(inv.Claims, payer)
		} else {
			inv.Claims[payer] = c
		}
	}
}

// rowMatch is the invoice a spreadsheet row belongs to.
type rowMatch struct {
	invoiceID string
	uncertain bool // matched by similarity only – the user should check it
}

// matchRows assigns spreadsheet rows to existing invoices. file is the
// spreadsheet's file name, sheets and rows give each row's origin.
//
// What the row says counts first (provider 30, invoice date 25, amount 30,
// treatment date 5); the recorded origin only breaks ties, so a series of
// equal invoices (weekly physiotherapy) stays matched by date when rows are
// inserted or sorted. Invoices deleted in the app take part when they came
// from this file, so that their row is not offered as new again.
func matchRows(rows []map[string]string, sheets []string, rowNums []int, file string, invoices []model.Invoice, payers []string) []rowMatch {
	type cand struct {
		row, inv, content, origin int
		certain                   bool
	}
	var cands []cand
	norm := func(s string) string {
		return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
	}
	type known struct {
		cur, base map[string]string
		provider  []string
		src       *model.ImportSource
	}
	ks := make([]known, len(invoices))
	for j := range invoices {
		inv := &invoices[j]
		k := known{cur: excelFields(inv, payers), src: inv.Source}
		k.provider = []string{norm(k.cur["provider"])}
		if inv.Source != nil {
			k.base = inv.Source.Values
			k.provider = append(k.provider, norm(k.base["provider"]))
		}
		ks[j] = k
	}
	same := func(k known, field, v string) bool {
		return v != "" && (k.cur[field] == v || k.base != nil && k.base[field] == v)
	}
	for i, row := range rows {
		prov := norm(row["provider"])
		for j, k := range ks {
			origin := k.src != nil && strings.EqualFold(k.src.File, file) && k.src.Sheet == sheets[i]
			if invoices[j].Deleted && !origin {
				continue
			}
			content := 0
			if prov != "" && slices.Contains(k.provider, prov) {
				content += 30
			}
			dateSame := same(k, "invoiceDate", row["invoiceDate"])
			if dateSame {
				content += 25
			}
			if same(k, "amount", row["amount"]) {
				content += 30
			}
			if same(k, "serviceFrom", row["serviceFrom"]) {
				content += 5
			}
			exact, near := origin && k.src.Row == rowNums[i], origin && abs(k.src.Row-rowNums[i]) <= 5
			bonus := 0
			switch {
			case exact:
				bonus = 2
			case near:
				bonus = 1
			}
			// Two fields agree; or one, in the row the invoice came from.
			if content >= 55 || exact && content >= 30 {
				// Certain: provider, date and amount agree, or the invoice
				// date agrees in (or next to) the row it came from.
				cands = append(cands, cand{row: i, inv: j, content: content, origin: bonus,
					certain: content >= 85 || (exact || near) && content >= 55 && dateSame})
			}
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int {
		if a.content != b.content {
			return b.content - a.content
		}
		return b.origin - a.origin
	})
	out := make([]rowMatch, len(rows))
	usedRow := make([]bool, len(rows))
	usedInv := make([]bool, len(invoices))
	for _, c := range cands {
		if usedRow[c.row] || usedInv[c.inv] {
			continue
		}
		usedRow[c.row], usedInv[c.inv] = true, true
		out[c.row] = rowMatch{invoiceID: invoices[c.inv].ID, uncertain: !c.certain}
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// fieldLabel names a field for the preview.
func fieldLabel(set *model.Settings, key string) string {
	labels := map[string]string{"provider": "Behandler", "invoiceDate": "Rechnungsdatum", "serviceFrom": "Behandlungsdatum",
		"amount": "Betrag", "paid": "Bezahlt am", "note": "Bemerkung"}
	if l, ok := labels[key]; ok {
		return l
	}
	kind, payer, _ := strings.Cut(key, ":")
	name := payer
	if p, ok := set.Payer(payer); ok {
		name = p.Name
	}
	if kind == "submitted" {
		return name + " eingereicht"
	}
	return name + " erstattet"
}

// fieldText shows a field value in German.
func fieldText(key, v string) string {
	if v == "" {
		return "–"
	}
	switch {
	case key == "amount" || strings.HasPrefix(key, "refund:"):
		if c, err := money.Parse(v); err == nil {
			return c.EUR()
		}
	case key == "invoiceDate" || key == "serviceFrom" || key == "paid" || strings.HasPrefix(key, "submitted:"):
		if d, err := civil.Parse(v); err == nil {
			return d.DE()
		}
	}
	return v
}
