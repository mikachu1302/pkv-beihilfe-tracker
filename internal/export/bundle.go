package export

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/phpdave11/gofpdf"

	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/civil"
	"github.com/mikachu1302/pkv-beihilfe-tracker/internal/money"
)

// BundleItem is one invoice of a submission bundle.
type BundleItem struct {
	Provider    string
	ServiceDate civil.Date
	InvoiceDate civil.Date
	Reference   string
	Amount      money.Cents
	Documents   []string // full paths of PDFs or images
}

// Bundle is a submission to one payer.
type Bundle struct {
	Payer string
	Date  civil.Date
	Items []BundleItem
}

// BundleResult reports what went into the PDF.
type BundleResult struct {
	Documents int      // documents included (PDFs and photos)
	Missing   []string // providers of invoices without a document that could be included
}

// imageExt are the photo types pdfcpu can put on a page. HEIC (iPhone) is
// not among them; such documents are reported as missing.
var imageExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".tif": true, ".tiff": true, ".webp": true}

// WriteBundle writes one PDF: a cover page listing the invoices, followed by
// their documents (PDF pages as they are, photos as pages of their own).
func WriteBundle(b Bundle, out string) (BundleResult, error) {
	var res BundleResult
	tmp, err := os.MkdirTemp("", "pkv-bundle-")
	if err != nil {
		return res, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	cover := filepath.Join(tmp, "00-deckblatt.pdf")
	if err := writeCover(b, cover); err != nil {
		return res, err
	}
	parts := []string{cover}
	for i, it := range b.Items {
		docs := pdfParts(it.Documents, tmp, fmt.Sprintf("%02d", i+1))
		if len(docs) == 0 {
			res.Missing = append(res.Missing, it.Provider)
		}
		res.Documents += len(docs)
		parts = append(parts, docs...)
	}
	if len(parts) == 1 {
		return res, copyFile(cover, out)
	}
	return res, api.MergeCreateFile(context.Background(), parts, out, false, nil)
}

// ErrNoDocuments means none of the documents could be put into a PDF.
var ErrNoDocuments = errors.New("keine der Dateien kann als PDF zusammengefasst werden")

// MergeDocuments writes the documents (PDFs and photos, in order) into one
// PDF – the pages of a multi-page document, to open them together. Files
// that cannot be included are left out; it reports how many went in.
func MergeDocuments(docs []string, out string) (int, error) {
	tmp, err := os.MkdirTemp("", "pkv-merge-")
	if err != nil {
		return 0, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	parts := pdfParts(docs, tmp, "p")
	switch len(parts) {
	case 0:
		return 0, ErrNoDocuments
	case 1:
		return 1, copyFile(parts[0], out)
	}
	return len(parts), api.MergeCreateFile(context.Background(), parts, out, false, nil)
}

// pdfParts returns the documents as PDF files: PDFs as they are, photos
// converted to a page each (written to tmp, named with prefix). Missing files
// and photos that cannot be converted (e.g. HEIC) are left out.
func pdfParts(docs []string, tmp, prefix string) []string {
	var parts []string
	for j, doc := range docs {
		ext := strings.ToLower(filepath.Ext(doc))
		switch {
		case ext == ".pdf":
			if _, err := os.Stat(doc); err == nil {
				parts = append(parts, doc)
			}
		case imageExt[ext]:
			page := filepath.Join(tmp, fmt.Sprintf("%s-%02d.pdf", prefix, j+1))
			if err := api.ImportImagesFile(context.Background(), []string{doc}, page, pdfcpu.DefaultImportConfig(), nil); err == nil {
				parts = append(parts, page)
			}
		}
	}
	return parts
}

func writeCover(b Bundle, path string) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("") // cp1252: umlauts and €
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 10, tr("Einreichung: "+b.Payer), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 11)
	pdf.CellFormat(0, 7, tr(fmt.Sprintf("Datum: %s · %d Rechnungen", b.Date.DE(), len(b.Items))), "", 1, "L", false, 0, "")
	pdf.Ln(4)

	widths := []float64{8, 62, 24, 24, 24, 28}
	head := []string{"Nr.", "Behandler", "Behandlung", "Rechnung", "Rechnungsnr.", "Betrag"}
	pdf.SetFont("Helvetica", "B", 9)
	pdf.SetFillColor(235, 238, 243)
	for i, h := range head {
		align := "L"
		if i == len(head)-1 {
			align = "R"
		}
		pdf.CellFormat(widths[i], 7, tr(h), "B", 0, align, true, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetFont("Helvetica", "", 9)
	var sum money.Cents
	for i, it := range b.Items {
		cells := []string{fmt.Sprint(i + 1), truncate(it.Provider, 38), it.ServiceDate.DE(), it.InvoiceDate.DE(), truncate(it.Reference, 14), it.Amount.DE() + " €"}
		for j, c := range cells {
			align := "L"
			if j == len(cells)-1 {
				align = "R"
			}
			pdf.CellFormat(widths[j], 6.5, tr(c), "B", 0, align, false, 0, "")
		}
		pdf.Ln(-1)
		sum += it.Amount
	}
	pdf.SetFont("Helvetica", "B", 9)
	pdf.CellFormat(widths[0]+widths[1]+widths[2]+widths[3]+widths[4], 7, tr("Summe"), "", 0, "L", false, 0, "")
	pdf.CellFormat(widths[5], 7, tr(sum.DE()+" €"), "", 1, "R", false, 0, "")
	pdf.Ln(6)
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetTextColor(100, 107, 118)
	pdf.MultiCell(0, 4.5, tr("Die Belege folgen in dieser Reihenfolge. Erstellt mit PKV Beihilfe Tracker."), "", "L", false)
	return pdf.OutputFileAndClose(path)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
