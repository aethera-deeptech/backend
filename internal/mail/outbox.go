package mail

import (
	"bytes"
	"html/template"
	"log/slog"
)

// Outbox is what the account routes call to send their emails. Each call returns at once.
type Outbox struct {
	queue *Queue
}

// NewOutbox returns an outbox that sends through the queue
func NewOutbox(queue *Queue) *Outbox {
	return &Outbox{queue: queue}
}

var verifyHTML = template.Must(template.New("verify").Parse(`<p>Ketuk tautan ini untuk memverifikasi email Anda dan mengaktifkan akun.</p>
<p><a href="{{.Link}}">Verifikasi email</a></p>
<p>Tautan ini berlaku 24 jam. Jika Anda tidak mendaftar di Aethera, abaikan email ini.</p>
`))

var resetHTML = template.Must(template.New("reset").Parse(`<p>Ketuk tautan ini untuk memilih kata sandi baru.</p>
<p><a href="{{.Link}}">Atur ulang kata sandi</a></p>
<p>Tautan ini berlaku 1 jam. Jika Anda tidak memintanya, abaikan email ini dan kata sandi Anda tetap sama.</p>
`))

// Verification queues the email with the link that verifies an address. The plain text part holds the plain URL.
func (o *Outbox) Verification(to, link string) {
	o.send(Message{
		To:      to,
		Subject: "Verifikasi email Aethera",
		Text:    "Buka tautan ini untuk memverifikasi email Anda dan mengaktifkan akun.\r\n\r\n" + link + "\r\n\r\nTautan ini berlaku 24 jam. Jika Anda tidak mendaftar di Aethera, abaikan email ini.\r\n",
		HTML:    render(verifyHTML, link),
	})
}

// PasswordReset queues the email with the link that sets a new password
func (o *Outbox) PasswordReset(to, link string) {
	o.send(Message{
		To:      to,
		Subject: "Atur ulang kata sandi Aethera",
		Text:    "Buka tautan ini untuk memilih kata sandi baru.\r\n\r\n" + link + "\r\n\r\nTautan ini berlaku 1 jam. Jika Anda tidak memintanya, abaikan email ini dan kata sandi Anda tetap sama.\r\n",
		HTML:    render(resetHTML, link),
	})
}

func (o *Outbox) send(m Message) {
	if !o.queue.Enqueue(m) {
		slog.Warn("an email was not queued")
	}
}

func render(t *template.Template, link string) string {
	var buf bytes.Buffer

	if err := t.Execute(&buf, struct{ Link string }{link}); err != nil {
		slog.Error("render an email", "error", err)
	}

	return buf.String()
}

// Log is the mailer of a development setup with no mail server. It writes the link to the log. The service refuses to start
// in production without a mail server, so this never runs there.
type Log struct{}

// Verification logs the link
func (Log) Verification(to, link string) { slog.Info("verification email", "to", to, "link", link) }

// PasswordReset logs the link
func (Log) PasswordReset(to, link string) { slog.Info("password reset email", "to", to, "link", link) }
