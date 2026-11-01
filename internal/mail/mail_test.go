package mail

import (
	"context"
	"errors"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flaky fails a number of times and then succeeds, and counts the sends
type flaky struct {
	failures int32
	sends    atomic.Int32
	sent     chan Message
}

func (f *flaky) Send(_ context.Context, m Message) error {
	if f.sends.Add(1) <= f.failures {
		return &textproto.Error{Code: 451, Msg: "try later for a@b.cd"}
	}

	f.sent <- m

	return nil
}

func TestAFailedMessageIsTriedAgainWithoutBlockingAWorker(t *testing.T) {
	f := &flaky{failures: 1, sent: make(chan Message, 4)}
	q := NewQueue(f, 1, 10)
	q.waits = []time.Duration{30 * time.Millisecond}

	q.Enqueue(Message{To: "a@b.cd", Subject: "first"})
	q.Enqueue(Message{To: "c@d.ef", Subject: "second"})

	// the one worker sends the second message while the first waits for its new try
	got := []string{(<-f.sent).Subject, (<-f.sent).Subject}
	if got[0] != "second" || got[1] != "first" {
		t.Fatalf("the order of the sends is %v, want the second message first", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	q.Close(ctx)
}

func TestACloseDropsTheMessagesThatWaitAndRefusesNewOnes(t *testing.T) {
	f := &flaky{failures: 100, sent: make(chan Message, 1)}
	q := NewQueue(f, 1, 10)
	q.waits = []time.Duration{time.Hour}

	q.Enqueue(Message{To: "a@b.cd"})

	for f.sends.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	q.Close(ctx)

	if q.Enqueue(Message{To: "x@y.zz"}) {
		t.Fatal("a closed queue took a message")
	}
}

func TestAFullQueueDropsTheMessage(t *testing.T) {
	block := make(chan struct{})
	q := NewQueue(senderFunc(func(context.Context, Message) error { <-block; return nil }), 1, 1)

	q.Enqueue(Message{To: "a@b.cd"})

	for q.jobsWaiting() > 0 {
		time.Sleep(time.Millisecond)
	}

	q.Enqueue(Message{To: "b@b.cd"})

	if q.Enqueue(Message{To: "c@b.cd"}) {
		t.Fatal("a full queue took a message")
	}

	close(block)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	q.Close(ctx)
}

type senderFunc func(context.Context, Message) error

func (f senderFunc) Send(ctx context.Context, m Message) error { return f(ctx, m) }

func (q *Queue) jobsWaiting() int { return len(q.jobs) }

func TestReasonNeverRepeatsTheText(t *testing.T) {
	for _, err := range []error{
		&textproto.Error{Code: 550, Msg: "<a@b.cd> Recipient address rejected"},
		errors.New("dial tcp 10.0.0.1:465: a@b.cd refused"),
	} {
		if r := Reason(err); strings.Contains(r, "a@b.cd") || strings.Contains(r, "10.0.0.1") {
			t.Errorf("the reason %q repeats the text of the error", r)
		}
	}

	if got := Reason(&textproto.Error{Code: 550}); got != "the mail server answered 550" {
		t.Errorf("reason is %q", got)
	}
}

func TestBuildMakesAMultipartMessageWithAPlainTextLink(t *testing.T) {
	s := &SMTP{FromName: "Aethera", FromAddress: "no-reply@mail.example.com"}

	raw, err := s.build(Message{To: "budi@example.com", Subject: "Verifikasi", Text: "Buka\r\n\r\nhttps://app.example.com/verify-email?token=abc&x=1\r\n", HTML: "<p>x</p>"})
	if err != nil {
		t.Fatal(err)
	}

	body := string(raw)
	for _, want := range []string{"From: ", "To: budi@example.com", "multipart/alternative", "text/plain", "text/html", "quoted-printable", "Message-ID: <"} {
		if !strings.Contains(body, want) {
			t.Errorf("the message lacks %q", want)
		}
	}

	if _, err := s.build(Message{To: "budi@example.com\r\nBcc: x@y.zz", Subject: "s"}); err == nil {
		t.Error("a line break in the recipient was accepted")
	}

	if _, err := s.build(Message{To: "budi@example.com", Subject: "s\r\nBcc: x@y.zz"}); err == nil {
		t.Error("a line break in the subject was accepted")
	}

	if _, err := s.build(Message{To: "Budi <budi@example.com>", Subject: "s"}); err == nil {
		t.Error("a recipient with a display name was accepted")
	}
}
