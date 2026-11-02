package tips

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// num writes a number the Indonesian way, with a comma for the decimals and a dot between the thousands
func num(v float64, decimals int) string {
	neg := v < 0
	s := strconv.FormatFloat(math.Abs(v), 'f', decimals, 64)

	whole, frac, _ := strings.Cut(s, ".")

	var b strings.Builder

	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte('.')
		}

		b.WriteRune(r)
	}

	out := b.String()
	if frac != "" {
		out += "," + frac
	}

	if neg {
		out = "-" + out
	}

	return out
}

// rp writes an amount in rupiah, for example Rp118.000
func rp(n int) string { return "Rp" + num(float64(n), 0) }

// text is everything that a tip says, which depends on its kind, its device and its state
type text struct {
	Title, WhatToDo, Why string
	How                  []string
}

// facts are the numbers and names that the texts use
type facts struct {
	kind, deviceKind, name, status string
	kwhMonth                       float64
	tariff                         int
	tariffDefault                  bool
	// the check of an applied tip
	beforeKwh, afterKwh  float64
	hasResult            bool
	percent, measuredKwh float64
}

func (f facts) tariffLabel() string {
	if f.tariffDefault {
		return "tarif bawaan"
	}

	return "tarif Anda"
}

func (f facts) title() string {
	if f.kind == KindStandby {
		return "Kurangi daya siaga " + f.name
	}

	return "Matikan " + f.name + " di luar jam kerja"
}

func (f facts) why() string {
	if f.kind == KindStandby {
		return f.name + " masih menarik daya siaga saat tidak dipakai, di luar jam kerja."
	}

	return f.name + " masih menarik daya di luar jam kerja, saat tidak ada pekerjaan di alat ini."
}

func (f facts) action() string {
	switch {
	case f.kind == KindStandby && f.deviceKind == "clamp":
		return "Matikan " + f.name + " dari sakelarnya saat tidak dipakai di luar jam kerja."
	case f.kind == KindStandby:
		return "Cabut colokan " + f.name + " saat tidak dipakai di luar jam kerja."
	case f.deviceKind == "clamp":
		return "Matikan " + f.name + " dari sakelarnya setelah jam kerja."
	default:
		return "Matikan " + f.name + " dari colokan setelah jam kerja."
	}
}

const disclaimer = " Anda yang memutuskan. Sistem hanya menyarankan dan tidak mematikan alat."

func (f facts) whatToDo() string {
	switch f.status {
	case "deferred":
		return "Anda menunda saran ini dan saran tetap ada sampai Anda memutuskan. " + f.action() + disclaimer
	case "applied":
		return "Lanjutkan kebiasaan baru sampai masa verifikasi selesai, supaya hasilnya bisa dibandingkan dengan benar. Catat produksi tiap hari."
	case "verified":
		return "Pertahankan kebiasaan ini. Anda bisa menerapkan cara yang sama pada alat lain."
	case "rejected":
		return "Anda menolak saran ini, jadi sistem tidak akan menyarankannya lagi untuk alat ini."
	default:
		return f.action() + disclaimer
	}
}

func (f facts) estimate() string {
	rpv := RoundRp(f.kwhMonth, f.tariff)

	return fmt.Sprintf("Hemat per bulan = energi yang dihemat dalam kWh, dikali tarif listrik per kWh. Hitungannya, %s kWh dikali %s Rp%s per kWh sama dengan sekitar %s per bulan.",
		num(f.kwhMonth, 1), f.tariffLabel(), num(float64(f.tariff), 0), rp(rpv))
}

func (f facts) how() []string {
	energy := fmt.Sprintf("Energi yang dihemat = rata-rata energi %s di luar jam kerja selama %d hari terakhir, dikali %d hari. Hitungannya, %s kWh per hari dikali %d hari sama dengan %s kWh.",
		f.name, BaselineDays, monthDays, num(f.kwhMonth/monthDays, 2), monthDays, num(f.kwhMonth, 1))

	switch {
	case f.status == "verified" && f.hasResult:
		rpv := RoundRp(f.measuredKwh, f.tariff)

		return []string{
			"Energi per potong = energi dalam kWh dibagi jumlah potong yang dibuat.",
			fmt.Sprintf("Penghematan energi = (sebelum dikurangi sesudah) dibagi sebelum, dikali 100%%. Hitungannya, %s kWh per potong sebelum dan %s sesudah berarti hemat %s%%.",
				num(f.beforeKwh, 2), num(f.afterKwh, 2), num(f.percent, 1)),
			fmt.Sprintf("Hemat per bulan = energi yang dihemat dalam kWh, dikali tarif listrik per kWh. Hitungannya, %s kWh dikali %s Rp%s per kWh sama dengan sekitar %s per bulan.",
				num(f.measuredKwh, 1), f.tariffLabel(), num(float64(f.tariff), 0), rp(rpv)),
		}
	case f.status == "applied":
		return []string{
			"Energi per potong = energi dalam kWh dibagi jumlah potong yang dibuat.",
			"Penghematan energi = (sebelum dikurangi sesudah) dibagi sebelum, dikali 100%. Angka Anda muncul setelah verifikasi selesai.",
			energy,
			f.estimate(),
		}
	default:
		return []string{energy, f.estimate()}
	}
}

func (f facts) all() text {
	return text{Title: f.title(), WhatToDo: f.whatToDo(), Why: f.why(), How: f.how()}
}
