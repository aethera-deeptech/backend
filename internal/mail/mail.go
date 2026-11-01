// Package mail sends the emails of the service over SMTP. A request never waits for the mail server, because a slow server would
// make the answer slow only for an address that has an account, and that would tell a stranger which emails exist.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The values of the setting SMTP_SECURITY
const (
	SecurityTLS      = "tls"
	SecuritySTARTTLS = "starttls"
	SecurityNone     = "none"
)

const (
	dialTimeout = 10 * time.Second
	sendTimeout = 30 * time.Second
)

// Message is one email with a plain text part and an HTML part
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Sender sends one message and waits for the answer of the mail server
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTP sends over a connection to a mail server
type SMTP struct {
	Host        string
	Port        uint
	Username    string
	Password    string
	Security    string
	FromName    string
	FromAddress string
}

// Send delivers the message. It opens a new connection each time, because the service sends few emails.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	body, err := s.build(m)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	addr := net.JoinHostPort(s.Host, strconv.FormatUint(uint64(s.Port), 10))
	dialer := &net.Dialer{Timeout: dialTimeout}

	var conn net.Conn

	if s.Security == SecurityTLS {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}

	if err != nil {
		return fmt.Errorf("connect to the mail server %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()

		return fmt.Errorf("start the mail session %w", err)
	}

	defer func() { _ = client.Close() }()

	if s.Security == SecuritySTARTTLS {
		if err := client.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start TLS %w", err)
		}
	}

	if s.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("sign in to the mail server %w", err)
		}
	}

	if err := client.Mail(s.FromAddress); err != nil {
		return fmt.Errorf("set the sender %w", err)
	}

	if err := client.Rcpt(m.To); err != nil {
		return fmt.Errorf("set the recipient %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("start the message %w", err)
	}

	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write the message %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("finish the message %w", err)
	}

	return client.Quit()
}

// build writes the message as MIME with a plain text part and an HTML part, both in quoted-printable
func (s *SMTP) build(m Message) ([]byte, error) {
	for _, v := range []string{m.To, m.Subject, s.FromAddress, s.FromName} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, errors.New("a header of the message has a line break")
		}
	}

	to, err := mail.ParseAddress(m.To)
	if err != nil || to.Address != m.To {
		return nil, errors.New("the recipient is not a plain email address")
	}

	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return nil, fmt.Errorf("make an id %w", err)
	}

	_, domain, _ := strings.Cut(s.FromAddress, "@")
	from := mail.Address{Name: s.FromName, Address: s.FromAddress}

	var buf bytes.Buffer

	mp := multipart.NewWriter(&buf)

	header := func(name, value string) { fmt.Fprintf(&buf, "%s: %s\r\n", name, value) }
	header("From", from.String())
	header("To", m.To)
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", time.Now().UTC().Format(time.RFC1123Z))
	header("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "multipart/alternative; boundary="+mp.Boundary())
	buf.WriteString("\r\n")

	for _, part := range []struct{ kind, text string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		w, err := mp.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.kind + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, fmt.Errorf("start a part %w", err)
		}

		qp := quotedprintable.NewWriter(w)
		if _, err := io.WriteString(qp, part.text); err != nil {
			return nil, fmt.Errorf("write a part %w", err)
		}

		if err := qp.Close(); err != nil {
			return nil, fmt.Errorf("close a part %w", err)
		}
	}

	if err := mp.Close(); err != nil {
		return nil, fmt.Errorf("close the message %w", err)
	}

	return buf.Bytes(), nil
}

// Queue sends messages in the background with a few workers and a few tries for each message
type Queue struct {
	sender  Sender
	jobs    chan Message
	wg      sync.WaitGroup
	waits   []time.Duration
	closeMu sync.Mutex
	closed  bool
}

// NewQueue starts the workers. A full queue drops the message and logs it, because a person can ask for the email again.
func NewQueue(sender Sender, workers, size int) *Queue {
	q := &Queue{sender: sender, jobs: make(chan Message, size), waits: []time.Duration{2 * time.Second, 10 * time.Second}}

	for range workers {
		q.wg.Go(q.work)
	}

	return q
}

// Enqueue adds a message without waiting. It reports false when the queue is full or closed.
func (q *Queue) Enqueue(m Message) bool {
	q.closeMu.Lock()
	defer q.closeMu.Unlock()

	if q.closed {
		return false
	}

	select {
	case q.jobs <- m:
		return true
	default:
		slog.Warn("the mail queue is full, a message was dropped")

		return false
	}
}

// Close stops taking messages and waits for the ones that are queued until the context ends
func (q *Queue) Close(ctx context.Context) {
	q.closeMu.Lock()
	if !q.closed {
		q.closed = true

		close(q.jobs)
	}
	q.closeMu.Unlock()

	done := make(chan struct{})

	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("the mail queue did not finish before the stop")
	}
}

func (q *Queue) work() {
	for m := range q.jobs {
		for attempt := 0; ; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
			err := q.sender.Send(ctx, m)

			cancel()

			if err == nil {
				break
			}

			// the address of the person is not written to the log
			slog.Warn("an email was not sent", "attempt", attempt+1, "error", err)

			if attempt >= len(q.waits) {
				break
			}

			time.Sleep(q.waits[attempt])
		}
	}
}
