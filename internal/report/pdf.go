package report

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/tips"
)

// A4 in points, with the margins of the design
const (
	marginX = 40.0
	marginY = 28.0
	pageW   = 595.28
	pageH   = 841.89
	gap     = 12.0
)

var shortMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// PDF returns the report of a month as one A4 page with the name that the app saves it under
func (s *Service) PDF(ctx context.Context, businessID, period string) ([]byte, string, error) {
	if !periodPattern.MatchString(period) {
		return nil, "", api.NotFound()
	}

	r, err := s.Get(ctx, businessID, period)
	if err != nil {
		return nil, "", err
	}

	if r.Phase != tips.PhaseReady || r.Saving == nil || r.Tariff == nil || r.Period != period {
		return nil, "", api.NotFound()
	}

	start, err := time.Parse("2006-01", period)
	if err != nil {
		return nil, "", api.NotFound()
	}

	var buf bytes.Buffer
	if err := render(&buf, r, start); err != nil {
		return nil, "", err
	}

	return buf.Bytes(), fmt.Sprintf("Laporan-%s-%d.pdf", monthNames[start.Month()-1], start.Year()), nil
}

func throughLabel(t time.Time) string {
	return fmt.Sprintf("%d %s", t.Day(), shortMonths[t.Month()-1])
}

func countLine(n int, none, some string) string {
	if n == 0 {
		return none
	}

	return fmt.Sprintf(some, n)
}

// render draws the page. It uses the built in Helvetica, so the file needs no font files, and the text is cut to the Windows Latin
// alphabet, which holds every letter of Indonesian. A letter outside it, in the name of a business, is drawn as a dot.
func render(buf *bytes.Buffer, r Report, start time.Time) error {
	pdf := fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt", Size: fpdf.SizeType{Wd: pageW, Ht: pageH}})
	pdf.SetMargins(marginX, marginY, marginX)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetTitle("Laporan penghematan energi", true)
	pdf.AddPage()

	tr := pdf.UnicodeTranslatorFromDescriptor("")
	width := pageW - 2*marginX
	label := monthLabel(start)

	if r.Through != nil {
		label += ", sampai " + throughLabel(*r.Through)
	}

	pdf.SetFont("Helvetica", "B", 20)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(width, 26, tr("Laporan penghematan energi"), "", 1, "L", false, 0, "")

	tariff := fmt.Sprintf("Rupiah memakai tarif bawaan Rp%s per kWh.", tips.Num(float64(r.Tariff.RpPerKwh), 0))
	if !r.Tariff.IsDefault {
		tariff = fmt.Sprintf("Rupiah memakai tarif Anda, Rp%s per kWh.", tips.Num(float64(r.Tariff.RpPerKwh), 0))
	}

	pdf.SetFont("Helvetica", "", 11)
	pdf.SetTextColor(70, 70, 70)
	pdf.MultiCell(width, 15, tr(fmt.Sprintf("%s, %s. %s Sistem hanya menyarankan dan tidak mengubah atau mematikan alat Anda.", r.businessName, label, tariff)), "", "L", false)
	pdf.Ln(gap)

	card := func(title string, height float64, body func(x, y float64)) {
		y := pdf.GetY()
		pdf.SetDrawColor(210, 210, 210)
		pdf.RoundedRect(marginX, y, width, height, 8, "1234", "D")
		pdf.SetFont("Helvetica", "B", 13)
		pdf.SetTextColor(20, 20, 20)
		pdf.SetXY(marginX+14, y+12)
		pdf.CellFormat(width-28, 16, tr(title), "", 0, "L", false, 0, "")
		body(marginX+14, y+36)
		pdf.SetY(y + height + gap)
	}

	row := func(x, y float64, tag, line string, rp int) {
		pdf.SetFont("Helvetica", "B", 11)
		pdf.SetXY(x, y)
		pdf.CellFormat(90, 14, tr(tag), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 11)
		pdf.SetTextColor(70, 70, 70)
		pdf.CellFormat(width-28-90-90, 14, tr(line), "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "B", 11)
		pdf.SetTextColor(20, 20, 20)
		pdf.CellFormat(90, 14, tr(tips.Rp(rp)), "", 0, "R", false, 0, "")
	}

	sv := r.Saving

	card("Hemat per bulan", 130, func(x, y float64) {
		row(x, y, "Terverifikasi", countLine(sv.Verified.Count, "Belum ada saran yang dicek", fmt.Sprintf("Sudah dicek %d hari, %%d saran", tips.VerifyDays)), sv.Verified.Rp)
		row(x, y+22, "Diterapkan", countLine(sv.Applied.Count, "Belum ada saran yang diterapkan",
			fmt.Sprintf("Sedang dicek, hari ke-%d dari %d, %%d saran", sv.Applied.Day, sv.Applied.NeededDays)), sv.Applied.Rp)
		row(x, y+44, "Perkiraan", countLine(sv.Estimate.Count, "Belum ada saran pada periode ini", "Menunggu keputusan Anda, %d saran"), sv.Estimate.Rp)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(100, 100, 100)
		pdf.SetXY(x, y+68)
		pdf.MultiCell(width-28, 12, tr("Terverifikasi berarti energinya sudah dicek dari data alat. Ketiganya dihitung terpisah."), "", "L", false)
	})

	if r.EnergyPerPiece != nil {
		e := r.EnergyPerPiece
		card("Energi per "+r.unit+", seluruh bengkel", 110, func(x, y float64) {
			bar(pdf, tr, x, y, width-28, "Sebelum", e.BeforeKwh, math.Max(e.BeforeKwh, e.AfterKwh), 2)
			bar(pdf, tr, x, y+22, width-28, "Sesudah", e.AfterKwh, math.Max(e.BeforeKwh, e.AfterKwh), 2)
			pdf.SetFont("Helvetica", "", 10)
			pdf.SetTextColor(70, 70, 70)
			pdf.SetXY(x, y+46)

			text := "kWh per " + r.unit
			switch {
			case e.BeforeKwh > 0 && e.AfterKwh < e.BeforeKwh:
				text += fmt.Sprintf(". Hemat %s%%", tips.Num((e.BeforeKwh-e.AfterKwh)/e.BeforeKwh*100, 1))
			case e.BeforeKwh > 0:
				text += ". Tidak ada penurunan"
			}

			pdf.CellFormat(width-28, 14, tr(text), "", 0, "L", false, 0, "")
		})
	}

	title := "Energi per alat, 4 teratas (kWh)"
	if r.Through != nil {
		title = "Energi per alat bulan ini, 4 teratas (kWh)"
	}

	top := sortedTop(*r.TopDevices)
	card(title, 52+22*float64(max(len(top), 1)), func(x, y float64) {
		if len(top) == 0 {
			pdf.SetFont("Helvetica", "", 10)
			pdf.SetXY(x, y)
			pdf.CellFormat(width-28, 14, tr("Belum ada energi yang terukur."), "", 0, "L", false, 0, "")

			return
		}

		for i, d := range top {
			bar(pdf, tr, x, y+22*float64(i), width-28, d.Name, d.Kwh, top[0].Kwh, 1)
		}
	})

	billLine := fmt.Sprintf("Tagihan %s Rp%s.", label, tips.Num(float64(*r.BillRp), 0))
	if r.Through != nil {
		billLine = fmt.Sprintf("Tagihan sampai %s %s.", throughLabel(*r.Through), tips.Rp(*r.BillRp))
	}

	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetTextColor(20, 20, 20)
	pdf.CellFormat(width, 18, tr(billLine), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(100, 100, 100)
	pdf.CellFormat(width, 14, tr("Tagihan dihitung dari energi yang diukur oleh alat Anda."), "", 1, "L", false, 0, "")

	if err := pdf.Error(); err != nil {
		return fmt.Errorf("draw the report %w", err)
	}

	if err := pdf.Output(buf); err != nil {
		return fmt.Errorf("write the report %w", err)
	}

	return nil
}

// bar draws a label, a bar as long as value against the largest value, and the value
func bar(pdf *fpdf.Fpdf, tr func(string) string, x, y, width float64, label string, value, largest float64, decimals int) {
	const labelW, valueW = 120.0, 50.0

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetTextColor(40, 40, 40)
	pdf.SetXY(x, y)

	// a name is cut by its width on the page and not by its length, because capitals are wider than small letters
	text := tr(label)
	if pdf.GetStringWidth(text) > labelW-6 {
		for len(text) > 1 && pdf.GetStringWidth(text+"...") > labelW-6 {
			text = text[:len(text)-1]
		}

		text = strings.TrimSpace(text) + "..."
	}

	pdf.CellFormat(labelW, 14, text, "", 0, "L", false, 0, "")

	room := width - labelW - valueW
	if largest > 0 && value > 0 {
		pdf.SetFillColor(60, 120, 90)
		pdf.Rect(x+labelW, y+2, math.Max(2, room*value/largest), 10, "F")
	}

	pdf.SetXY(x+width-valueW, y)
	pdf.CellFormat(valueW, 14, tr(tips.Num(value, decimals)), "", 0, "R", false, 0, "")
}
