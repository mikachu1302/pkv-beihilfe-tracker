package match

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/model"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

var d = civil.MustParse

func eur(s string) money.Cents {
	c, err := money.Parse(s)
	if err != nil {
		panic(err)
	}
	return c
}

func settings() *model.Settings {
	return &model.Settings{
		Payers: []model.Payer{{ID: "bh", Kind: model.Beihilfe, Name: "Beihilfe"}, {ID: "pkv", Kind: model.PKV, Name: "PKV"}},
		Coverage: []model.CoveragePeriod{{From: d("2020-01-01"), Shares: []model.Share{
			{PayerID: "bh", Rate: money.Percent(70)}, {PayerID: "pkv", Rate: money.Percent(30)}}}},
	}
}

func submitted(on string) map[string]model.Claim {
	return map[string]model.Claim{"bh": {Submitted: model.Mark{Date: d(on)}}}
}

func inv(id, provider, invoiceDate, serviceDate, amount string) model.Invoice {
	i := model.Invoice{ID: id, Provider: provider, InvoiceDate: d(invoiceDate), Amount: eur(amount), Claims: submitted("2026-04-02")}
	if serviceDate != "" {
		i.ServiceFrom = d(serviceDate)
	}
	return i
}

func byLine(rs []Result) map[string]Result {
	m := map[string]Result{}
	for _, r := range rs {
		m[r.LineID] = r
	}
	return m
}

func TestProposalExample(t *testing.T) {
	invoices := []model.Invoice{
		inv("muster", "Dr. Muster", "2026-03-14", "2026-03-03", "286,40"),
		inv("markt", "Apotheke am Markt", "2026-03-05", "", "45,10"),
		inv("markt2", "Apotheke am Markt", "2026-03-10", "", "12,50"),
		inv("sonne", "Sonnen-Apotheke", "2026-03-10", "", "12,50"),
		inv("weit", "Dr. Weit", "2026-01-20", "", "99,00"),
	}
	lines := []Line{
		{ID: "1", Date: d("2026-03-03"), Provider: "Dr. med. Muster", Billed: eur("286,40"), Refund: eur("180,20")},
		{ID: "2", Date: d("2026-03-05"), Provider: "Apotheke a. Markt", Billed: eur("45,10"), Refund: eur("31,57")},
		{ID: "3", Date: d("2026-03-10"), Billed: eur("12,50"), Refund: eur("8,75")},
		{ID: "4", Date: d("2026-03-18"), Provider: "Labor XY", Billed: eur("420,00"), Refund: eur("294,00")},
	}
	r := byLine(Match(lines, invoices, settings(), Options{PayerID: "bh"}))
	if got := r["1"]; got.Confidence != Auto || !slices.Equal(got.InvoiceIDs, []string{"muster"}) {
		t.Errorf("line 1: %+v", got)
	}
	if got := r["2"]; got.Confidence != Auto || !slices.Equal(got.InvoiceIDs, []string{"markt"}) {
		t.Errorf("line 2: %+v", got)
	}
	got := r["3"]
	if got.Confidence != Suggest || len(got.InvoiceIDs) != 1 || len(got.Candidates) < 2 {
		t.Errorf("line 3 should be an ambiguous suggestion: %+v", got)
	}
	cands := []string{got.Candidates[0].InvoiceID, got.Candidates[1].InvoiceID}
	slices.Sort(cands)
	if !slices.Equal(cands, []string{"markt2", "sonne"}) {
		t.Errorf("line 3 candidates: %v", cands)
	}
	if got := r["4"]; got.Confidence != None || got.InvoiceIDs != nil {
		t.Errorf("line 4 should stay unassigned: %+v", got)
	}
	if r := Match(lines, invoices, settings(), Options{PayerID: "bh", ReviewAll: true}); r[0].Confidence != Suggest {
		t.Error("ReviewAll must not return auto")
	}
}

func TestNearAmountIsNeverAuto(t *testing.T) {
	invoices := []model.Invoice{inv("a", "Dr. Muster", "2026-03-14", "", "286,40")}
	for _, billed := range []string{"286,48", "236,40"} { // 1 %, one wrong digit
		r := Match([]Line{{ID: "1", Date: d("2026-03-14"), Billed: eur(billed), Refund: eur("100")}}, invoices, settings(), Options{PayerID: "bh"})
		if r[0].Confidence != Suggest || r[0].InvoiceIDs[0] != "a" {
			t.Errorf("%s: %+v", billed, r[0])
		}
	}
}

func TestPrefersOpenClaim(t *testing.T) {
	done := inv("done", "Apotheke", "2026-03-10", "", "12,50")
	done.Claims = map[string]model.Claim{"bh": {Refunds: []model.Refund{{ID: "x", Amount: eur("8,75")}}}}
	open := inv("open", "Apotheke", "2026-03-10", "", "12,50")
	r := Match([]Line{{ID: "1", Date: d("2026-03-10"), Billed: eur("12,50"), Refund: eur("8,75")}},
		[]model.Invoice{done, open}, settings(), Options{PayerID: "bh"})
	if r[0].InvoiceIDs[0] != "open" || r[0].Confidence != Auto {
		t.Errorf("%+v", r[0])
	}
}

func TestRefundAboveInvoiceExcluded(t *testing.T) {
	r := Match([]Line{{ID: "1", Date: d("2026-03-14"), Billed: eur("286,40"), Refund: eur("500")}},
		[]model.Invoice{inv("a", "X", "2026-03-14", "", "286,40")}, settings(), Options{PayerID: "bh"})
	if r[0].Confidence != None || len(r[0].Candidates) != 0 {
		t.Errorf("%+v", r[0])
	}
}

func TestOtherPayerAndDeletedIgnored(t *testing.T) {
	deleted := inv("del", "X", "2026-03-14", "", "50")
	deleted.Deleted = true
	pkvOnly := &model.Settings{Payers: settings().Payers, Coverage: []model.CoveragePeriod{
		{From: d("2020-01-01"), Shares: []model.Share{{PayerID: "pkv", Rate: money.Full}}}}}
	other := inv("other", "X", "2026-03-14", "", "50")
	other.Claims = nil
	r := Match([]Line{{ID: "1", Date: d("2026-03-14"), Billed: eur("50")}}, []model.Invoice{deleted, other}, pkvOnly, Options{PayerID: "bh"})
	if r[0].Confidence != None {
		t.Errorf("%+v", r[0])
	}
}

func TestAlias(t *testing.T) {
	s := settings()
	s.Aliases = []model.Alias{{From: "PVS Süd eG", To: "Dr. Muster"}}
	invoices := []model.Invoice{inv("a", "Dr. Muster", "2026-03-14", "", "100"), inv("b", "Dr. Anders", "2026-03-14", "", "100")}
	r := Match([]Line{{ID: "1", Date: d("2026-03-14"), Provider: "PVS Süd eG", Billed: eur("100"), Refund: eur("70")}}, invoices, s, Options{PayerID: "bh"})
	if r[0].InvoiceIDs[0] != "a" || r[0].Confidence != Suggest { // alias gives 15 points, not enough lead for auto
		t.Errorf("%+v", r[0])
	}
}

func TestSeveralLinesOneInvoice(t *testing.T) {
	invoices := []model.Invoice{inv("zahn", "Zahnarztpraxis", "2026-02-20", "2026-02-10", "1000")}
	lines := []Line{
		{ID: "fee", Date: d("2026-02-10"), Billed: eur("600"), Refund: eur("420")},
		{ID: "material", Date: d("2026-02-10"), Billed: eur("250"), Refund: eur("100")},
		{ID: "lab", Date: d("2026-02-10"), Billed: eur("150"), Refund: eur("105")},
	}
	for _, r := range Match(lines, invoices, settings(), Options{PayerID: "bh"}) {
		if r.Confidence != Suggest || !slices.Equal(r.InvoiceIDs, []string{"zahn"}) {
			t.Errorf("%s: %+v", r.LineID, r)
		}
	}
}

func TestOneLineSeveralInvoices(t *testing.T) {
	invoices := []model.Invoice{inv("p1", "Apotheke", "2026-05-02", "", "20"), inv("p2", "Apotheke", "2026-05-02", "", "37,60"),
		inv("p3", "Apotheke", "2026-09-01", "", "37,60")}
	r := Match([]Line{{ID: "1", Date: d("2026-05-02"), Billed: eur("57,60"), Refund: eur("40,32")}}, invoices, settings(), Options{PayerID: "bh"})
	ids := slices.Clone(r[0].InvoiceIDs)
	slices.Sort(ids)
	if r[0].Confidence != Suggest || !slices.Equal(ids, []string{"p1", "p2"}) {
		t.Errorf("%+v", r[0])
	}
}

func TestNormalizeAndDice(t *testing.T) {
	if got := normalize("Dr. med. Jürgen Müller-Lüdenscheidt GmbH"); got != "juergen mueller luedenscheidt" {
		t.Errorf("normalize = %q", got)
	}
	if dice("apotheke am markt", "apotheke am markt") != 1 || dice("abc", "xyz") != 0 {
		t.Error("dice")
	}
}

// TestHungarianOptimal compares the assignment with brute force on random matrices.
func TestHungarianOptimal(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for iter := 0; iter < 300; iter++ {
		n, m := 1+rng.IntN(4), 1+rng.IntN(5)
		scores := make([][]int, n)
		for i := range scores {
			scores[i] = make([]int, m)
			for j := range scores[i] {
				scores[i][j] = rng.IntN(111)
			}
		}
		minScore := 40
		got := 0
		used := map[int]bool{}
		for i, j := range assign(scores, minScore) {
			if j >= 0 {
				if used[j] || scores[i][j] < minScore {
					t.Fatalf("invalid assignment %v for %v", j, scores)
				}
				used[j] = true
				got += scores[i][j] - minScore + 1
			}
		}
		if want := brute(scores, minScore, 0, map[int]bool{}); got != want {
			t.Fatalf("iteration %d: got %d, want %d for %v", iter, got, want, scores)
		}
	}
}

// brute returns the best total of (score − minScore + 1) over all partial assignments.
func brute(scores [][]int, minScore, i int, used map[int]bool) int {
	if i == len(scores) {
		return 0
	}
	best := brute(scores, minScore, i+1, used) // line i unassigned
	for j, sc := range scores[i] {
		if !used[j] && sc >= minScore {
			used[j] = true
			best = max(best, sc-minScore+1+brute(scores, minScore, i+1, used))
			used[j] = false
		}
	}
	return best
}
