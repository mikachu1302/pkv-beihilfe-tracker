// Package match assigns the lines of a refund document (Beihilfebescheid,
// PKV-Leistungsabrechnung) to invoices by amount, date, provider and claim state.
//
// Every line gets a score per candidate invoice. Lines that add up exactly to
// one invoice (or one line that covers several invoices) are suggested as
// groups first, as long as none of them has an exact amount partner of its
// own. The rest is assigned 1:1, maximising the total score (Hungarian
// algorithm). Only unambiguous exact matches are marked "auto".
package match

import (
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// Line is a position read from a refund document.
type Line struct {
	ID       string
	Date     civil.Date
	Provider string
	Billed   money.Cents // invoice amount as printed on the document (0 if not shown)
	Refund   money.Cents
}

// Confidence of a result.
type Confidence string

// Confidences.
const (
	Auto    Confidence = model.MatchAuto    // can be taken over without review
	Suggest Confidence = model.MatchSuggest // plausible, needs a look
	None    Confidence = model.MatchNone    // nothing found
)

// Candidate is one possible invoice for a line.
type Candidate struct {
	InvoiceID string   `json:"invoiceId"`
	Score     int      `json:"score"`
	Exact     bool     `json:"exact"` // amount and date match exactly
	Reasons   []string `json:"reasons"`
}

// Result is the outcome for one line.
type Result struct {
	LineID     string      `json:"lineId"`
	InvoiceIDs []string    `json:"invoiceIds"`
	Confidence Confidence  `json:"confidence"`
	Score      int         `json:"score"`
	Candidates []Candidate `json:"candidates"` // best first, for manual choice
}

// Options tune the matching.
type Options struct {
	PayerID    string
	MinScore   int  // pairs below are never assigned (default 40)
	AutoMargin int  // "auto" needs this lead over the runner-up (default 20)
	ReviewAll  bool // never return "auto"
}

// Scores.
const (
	scoreAmountExact = 50
	scoreAmountNear  = 20
	scoreDateExact   = 30
	scoreDateNear    = 10
	scoreProvider    = 15
	scoreOpenClaim   = 10
	scoreRate        = 5
	penaltyRefunded  = 30
	penaltySkipped   = 20
	maxScore         = scoreAmountExact + scoreDateExact + scoreProvider + scoreOpenClaim + scoreRate

	nearDays      = 3
	maxCandidates = 5
	maxGroup      = 4
	maxPool       = 30
	groupScore    = scoreAmountExact + scoreDateNear
)

// Match assigns lines to invoices. Only invoices that have (or would have) a
// claim towards opt.PayerID are considered.
func Match(lines []Line, invoices []model.Invoice, s *model.Settings, opt Options) []Result {
	if opt.MinScore == 0 {
		opt.MinScore = 40
	}
	if opt.AutoMargin == 0 {
		opt.AutoMargin = 20
	}
	// One name on a refund document may stand for several providers (a
	// billing service for several doctors).
	aliases := map[string]map[string]bool{}
	for _, a := range s.Aliases {
		from := normalize(a.From)
		if aliases[from] == nil {
			aliases[from] = map[string]bool{}
		}
		aliases[from][normalize(a.To)] = true
	}
	var cands []*model.Invoice
	for i := range invoices {
		inv := &invoices[i]
		if !inv.Deleted && slices.Contains(inv.PayerIDs(s), opt.PayerID) {
			cands = append(cands, inv)
		}
	}

	n, m := len(lines), len(cands)
	scores := make([][]int, n)
	exact := make([][]bool, n)
	results := make([]Result, n)
	for i, l := range lines {
		scores[i] = make([]int, m)
		exact[i] = make([]bool, m)
		results[i] = Result{LineID: l.ID, Confidence: None}
		for j, inv := range cands {
			sc, ex, reasons := score(l, inv, s, opt.PayerID, aliases)
			scores[i][j], exact[i][j] = sc, ex
			if sc > 0 {
				results[i].Candidates = append(results[i].Candidates,
					Candidate{InvoiceID: inv.ID, Score: sc, Exact: ex, Reasons: reasons})
			}
		}
		slices.SortStableFunc(results[i].Candidates, func(a, b Candidate) int { return b.Score - a.Score })
		if len(results[i].Candidates) > maxCandidates {
			results[i].Candidates = results[i].Candidates[:maxCandidates]
		}
	}

	// Groups first: an exact sum is a stronger signal than a 1:1 pair that
	// only agrees on the date. Lines and invoices that have an exact amount
	// partner of their own are left to the 1:1 assignment.
	lineUsed := make([]bool, n)
	invUsed := make([]bool, m)
	for i, l := range lines {
		for j, c := range cands {
			if l.Billed != 0 && l.Billed == c.Amount && scores[i][j] > 0 {
				lineUsed[i], invUsed[j] = true, true
			}
		}
	}
	lineExact, invExact := slices.Clone(lineUsed), slices.Clone(invUsed)
	groupLines(lines, cands, opt.PayerID, lineUsed, invUsed, results)
	groupInvoices(lines, cands, opt.PayerID, lineUsed, invUsed, results)

	// 1:1 assignment of everything not grouped.
	var rows, cols []int
	for i := range lines {
		if !lineUsed[i] || lineExact[i] {
			rows = append(rows, i)
		}
	}
	for j := range cands {
		if !invUsed[j] || invExact[j] {
			cols = append(cols, j)
		}
	}
	sub := make([][]int, len(rows))
	for a, i := range rows {
		sub[a] = make([]int, len(cols))
		for b, j := range cols {
			sub[a][b] = scores[i][j]
		}
	}
	for a, b := range assign(sub, opt.MinScore) {
		if b < 0 {
			continue
		}
		i, j := rows[a], cols[b]
		r := &results[i]
		r.InvoiceIDs = []string{cands[j].ID}
		r.Score = scores[i][j]
		r.Confidence = Suggest
		if !opt.ReviewAll && exact[i][j] && lead(scores, i, j) >= opt.AutoMargin {
			r.Confidence = Auto
		}
	}
	return results
}

// lead is how far pair (i, j) is ahead of every competing pair in row i and column j.
func lead(scores [][]int, i, j int) int {
	best := 0
	for jj, sc := range scores[i] {
		if jj != j && sc > best {
			best = sc
		}
	}
	for ii := range scores {
		if ii != i && scores[ii][j] > best {
			best = scores[ii][j]
		}
	}
	return scores[i][j] - best
}

func score(l Line, inv *model.Invoice, s *model.Settings, payerID string, aliases map[string]map[string]bool) (int, bool, []string) {
	if l.Refund > 0 && inv.Amount > 0 && l.Refund > inv.Amount {
		return 0, false, nil // a refund can never exceed the invoice
	}
	sc := 0
	var reasons []string
	amountExact := l.Billed != 0 && l.Billed == inv.Amount
	switch {
	case amountExact:
		sc += scoreAmountExact
		reasons = append(reasons, "Betrag stimmt")
	case near(l.Billed, inv.Amount):
		sc += scoreAmountNear
		reasons = append(reasons, "Betrag fast gleich")
	}
	dateExact := inv.Matches(l.Date)
	switch {
	case dateExact:
		sc += scoreDateExact
		reasons = append(reasons, "Datum stimmt")
	case dateNear(l.Date, inv):
		sc += scoreDateNear
		reasons = append(reasons, "Datum nah")
	}
	if sim := providerSimilarity(l.Provider, inv, aliases); sim > 0 {
		sc += int(math.Round(scoreProvider * sim))
		reasons = append(reasons, "Name ähnlich")
	}
	c := inv.Claims[payerID]
	switch {
	case c.Skip:
		sc -= penaltySkipped
		reasons = append(reasons, "als nicht einzureichen markiert")
	case c.Refunded.Done() || len(c.Refunds) > 0:
		sc -= penaltyRefunded
		reasons = append(reasons, "bereits erstattet")
	case c.Submitted.Done():
		sc += scoreOpenClaim
		reasons = append(reasons, "eingereicht, noch offen")
	}
	if l.Billed > 0 && l.Refund > 0 {
		want := s.ShareOf(payerID, inv.ServiceDate())
		if c.Rate != nil {
			want = *c.Rate
		}
		if d := money.RateOf(l.Refund, l.Billed) - want; want > 0 && d >= -500 && d <= 500 {
			sc += scoreRate
		}
	}
	return max(sc, 0), amountExact && dateExact, reasons
}

// near reports amounts that differ by at most 1 % or in a single digit (typical OCR errors).
func near(a, b money.Cents) bool {
	if a <= 0 || b <= 0 || a == b {
		return false
	}
	if (a-b).Abs()*100 <= b {
		return true
	}
	sa, sb := a.String(), b.String()
	if len(sa) != len(sb) {
		return false
	}
	diff := 0
	for i := range sa {
		if sa[i] != sb[i] {
			diff++
		}
	}
	return diff == 1
}

func dateNear(d civil.Date, inv *model.Invoice) bool {
	if d.IsZero() {
		return false
	}
	for _, x := range []civil.Date{inv.InvoiceDate, inv.ServiceFrom, inv.ServiceTo} {
		if !x.IsZero() {
			if n := d.DaysUntil(x); n >= -nearDays && n <= nearDays {
				return true
			}
		}
	}
	return false
}

// compatible: a line date that could belong to an invoice whose claim
// towards payer is still open (used for groups). Lines without a date are
// never grouped: every invoice would be compatible, and searching all
// subsets of them takes far too long.
func compatible(d civil.Date, inv *model.Invoice, payer string) bool {
	if d.IsZero() {
		return false
	}
	if c := inv.Claims[payer]; c.Refunded.Done() || len(c.Refunds) > 0 || c.Skip {
		return false
	}
	return inv.Matches(d) || dateNear(d, inv)
}

var stopwords = map[string]bool{
	"dr": true, "med": true, "dent": true, "prof": true, "praxis": true, "gmbh": true,
	"mvz": true, "und": true, "fuer": true, "frau": true, "herr": true, "fa": true,
}

// normalize lowercases, folds umlauts and drops titles and legal forms.
func normalize(s string) string {
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
		"Ä", "ae", "Ö", "oe", "Ü", "ue").Replace(strings.ToLower(s))
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := words[:0]
	for _, w := range words {
		if !stopwords[w] {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}

func providerSimilarity(name string, inv *model.Invoice, aliases map[string]map[string]bool) float64 {
	n := normalize(name)
	if n == "" {
		return 0
	}
	best := 0.0
	for _, other := range []string{inv.Provider, inv.Payee} {
		o := normalize(other)
		if o == "" {
			continue
		}
		if aliases[n][o] {
			return 1
		}
		best = max(best, dice(n, o))
	}
	if best < 0.4 {
		return 0
	}
	return best
}

// dice is the Dice coefficient of the character bigrams of a and b (spaces ignored).
func dice(a, b string) float64 {
	bigrams := func(s string) map[string]int {
		r := []rune(strings.ReplaceAll(s, " ", ""))
		m := map[string]int{}
		for i := 0; i+1 < len(r); i++ {
			m[string(r[i:i+2])]++
		}
		return m
	}
	ba, bb := bigrams(a), bigrams(b)
	total, common := 0, 0
	for k, v := range ba {
		total += v
		common += min(v, bb[k])
	}
	for _, v := range bb {
		total += v
	}
	if total == 0 {
		return 0
	}
	return 2 * float64(common) / float64(total)
}

// assign returns, for every line, the index of its invoice or -1.
func assign(scores [][]int, minScore int) []int {
	n := len(scores)
	out := make([]int, n)
	for i := range out {
		out[i] = -1
	}
	if n == 0 || len(scores[0]) == 0 {
		return out
	}
	m := len(scores[0])
	const forbidden = 1_000_000
	// Columns: m invoices plus n "no match" columns, so every line can stay unassigned.
	cost := make([][]int, n)
	for i := range cost {
		cost[i] = make([]int, m+n)
		for j := range m {
			if scores[i][j] >= minScore {
				cost[i][j] = maxScore - scores[i][j]
			} else {
				cost[i][j] = forbidden
			}
		}
		for j := m; j < m+n; j++ {
			cost[i][j] = maxScore - minScore + 1
		}
	}
	for i, j := range hungarian(cost) {
		if j < m && cost[i][j] < forbidden {
			out[i] = j
		}
	}
	return out
}

// hungarian solves the assignment problem for a cost matrix with rows ≤ columns
// and returns the column of every row (O(n²m), potentials method).
func hungarian(cost [][]int) []int {
	n, m := len(cost), len(cost[0])
	const inf = math.MaxInt / 4
	u, v := make([]int, n+1), make([]int, m+1)
	p, way := make([]int, m+1), make([]int, m+1)
	for i := 1; i <= n; i++ {
		p[0] = i
		j0 := 0
		minv := make([]int, m+1)
		used := make([]bool, m+1)
		for j := range minv {
			minv[j] = inf
		}
		for {
			used[j0] = true
			i0, delta, j1 := p[j0], inf, 0
			for j := 1; j <= m; j++ {
				if used[j] {
					continue
				}
				if cur := cost[i0-1][j-1] - u[i0] - v[j]; cur < minv[j] {
					minv[j], way[j] = cur, j0
				}
				if minv[j] < delta {
					delta, j1 = minv[j], j
				}
			}
			for j := 0; j <= m; j++ {
				if used[j] {
					u[p[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			j0 = j1
			if p[j0] == 0 {
				break
			}
		}
		for j0 != 0 {
			j1 := way[j0]
			p[j0] = p[j1]
			j0 = j1
		}
	}
	ans := make([]int, n)
	for j := 1; j <= m; j++ {
		if p[j] != 0 {
			ans[p[j]-1] = j - 1
		}
	}
	return ans
}

// groupLines finds several unassigned lines that together make up one unassigned invoice
// (e.g. a dental bill split into fee, material and lab).
func groupLines(lines []Line, cands []*model.Invoice, payer string, lineUsed, invUsed []bool, results []Result) {
	for j, inv := range cands {
		if invUsed[j] || inv.Amount <= 0 {
			continue
		}
		var pool []int
		var amounts []money.Cents
		for i, l := range lines {
			if !lineUsed[i] && l.Billed > 0 && compatible(l.Date, inv, payer) {
				pool = append(pool, i)
				amounts = append(amounts, l.Billed)
			}
		}
		subset := findSubset(amounts, inv.Amount)
		if subset == nil {
			continue
		}
		invUsed[j] = true
		for _, k := range subset {
			i := pool[k]
			lineUsed[i] = true
			results[i].InvoiceIDs = []string{inv.ID}
			results[i].Confidence = Suggest
			results[i].Score = groupScore
		}
	}
}

// groupInvoices finds one unassigned line that covers several unassigned invoices.
func groupInvoices(lines []Line, cands []*model.Invoice, payer string, lineUsed, invUsed []bool, results []Result) {
	for i, l := range lines {
		if lineUsed[i] || l.Billed <= 0 {
			continue
		}
		var pool []int
		var amounts []money.Cents
		for j, inv := range cands {
			if !invUsed[j] && inv.Amount > 0 && compatible(l.Date, inv, payer) {
				pool = append(pool, j)
				amounts = append(amounts, inv.Amount)
			}
		}
		subset := findSubset(amounts, l.Billed)
		if subset == nil {
			continue
		}
		lineUsed[i] = true
		results[i].InvoiceIDs = nil
		for _, k := range subset {
			invUsed[pool[k]] = true
			results[i].InvoiceIDs = append(results[i].InvoiceIDs, cands[pool[k]].ID)
		}
		results[i].Confidence = Suggest
		results[i].Score = groupScore
	}
}

// findSubset returns indices of 2..maxGroup amounts that add up to target
// (smallest groups first), or nil. Larger pools than maxPool are not
// searched (the number of subsets grows with the fourth power).
func findSubset(amounts []money.Cents, target money.Cents) []int {
	if len(amounts) > maxPool {
		return nil
	}
	for size := 2; size <= maxGroup && size <= len(amounts); size++ {
		if s := subsetOfSize(amounts, target, size, 0, nil); s != nil {
			return s
		}
	}
	return nil
}

func subsetOfSize(amounts []money.Cents, target money.Cents, size, start int, chosen []int) []int {
	if size == 0 {
		if target == 0 {
			return slices.Clone(chosen)
		}
		return nil
	}
	for k := start; k <= len(amounts)-size; k++ {
		if amounts[k] > target {
			continue
		}
		if s := subsetOfSize(amounts, target-amounts[k], size-1, k+1, append(chosen, k)); s != nil {
			return s
		}
	}
	return nil
}
