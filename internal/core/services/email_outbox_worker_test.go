package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// fakeOutbox simula email_outbox: ClaimDueEmails devuelve los pendientes
// vencidos y las marcas actualizan su estado.
type fakeOutbox struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*fakeOutboxRow
}

type fakeOutboxRow struct {
	sqlc.ClaimDueEmailsRow
	next   time.Time
	sent   bool
	failed bool
}

func newFakeOutbox(n int) *fakeOutbox {
	f := &fakeOutbox{rows: map[uuid.UUID]*fakeOutboxRow{}}
	for i := 0; i < n; i++ {
		id := uuid.New()
		f.rows[id] = &fakeOutboxRow{ClaimDueEmailsRow: sqlc.ClaimDueEmailsRow{
			ID: id, ToAddress: "a@b.c", Subject: "s", TextBody: "t", HtmlBody: "<p>t</p>",
		}}
	}
	return f
}

func (f *fakeOutbox) WithTx(ctx context.Context, fn func(q ports.OutboxQueries) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fn(f)
}

func (f *fakeOutbox) ClaimDueEmails(_ context.Context, limit int32) ([]sqlc.ClaimDueEmailsRow, error) {
	var out []sqlc.ClaimDueEmailsRow
	for _, r := range f.rows {
		if !r.sent && !r.failed && !r.next.After(time.Now()) && int32(len(out)) < limit {
			out = append(out, r.ClaimDueEmailsRow)
		}
	}
	return out, nil
}

func (f *fakeOutbox) MarkEmailSent(_ context.Context, id uuid.UUID) error {
	f.rows[id].sent = true
	f.rows[id].Attempts++
	return nil
}

func (f *fakeOutbox) MarkEmailRetry(_ context.Context, arg sqlc.MarkEmailRetryParams) error {
	f.rows[arg.ID].next = arg.NextAttemptAt
	f.rows[arg.ID].Attempts++
	return nil
}

func (f *fakeOutbox) MarkEmailFailed(_ context.Context, arg sqlc.MarkEmailFailedParams) error {
	f.rows[arg.ID].failed = true
	f.rows[arg.ID].Attempts++
	return nil
}

func (f *fakeOutbox) only() *fakeOutboxRow {
	for _, r := range f.rows {
		return r
	}
	return nil
}

// fakeMailer registra los envíos y falla si err no es nil.
type fakeMailer struct {
	sent []domain.Email
	err  error
}

func (m *fakeMailer) Send(_ context.Context, e domain.Email) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, e)
	return nil
}

func TestOutboxEnviaYMarca(t *testing.T) {
	store, mailer := newFakeOutbox(1), &fakeMailer{}
	w := NewEmailOutboxWorker(store, mailer, EmailOutboxWorkerConfig{})
	if _, err := w.processBatch(context.Background()); err != nil {
		t.Fatalf("processBatch: %v", err)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].HTML != "<p>t</p>" {
		t.Fatalf("envío inesperado: %+v", mailer.sent)
	}
	if !store.only().sent {
		t.Error("el correo debe marcarse como enviado")
	}
}

func TestOutboxReintentaConBackoffYFalla(t *testing.T) {
	store, mailer := newFakeOutbox(1), &fakeMailer{err: errors.New("smtp caído")}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	w := NewEmailOutboxWorker(store, mailer, EmailOutboxWorkerConfig{MaxAttempts: 3, RetryBase: 30 * time.Second})
	w.now = func() time.Time { return now }

	row := store.only()
	for i, want := range []time.Duration{30 * time.Second, time.Minute} {
		row.next = time.Time{} // vencido
		if _, err := w.processBatch(context.Background()); err != nil {
			t.Fatalf("processBatch: %v", err)
		}
		if got := row.next.Sub(now); got != want {
			t.Errorf("intento %d: backoff %v, esperaba %v", i+1, got, want)
		}
		if row.failed {
			t.Fatalf("intento %d: no debe marcarse fallido aún", i+1)
		}
	}
	row.next = time.Time{}
	if _, err := w.processBatch(context.Background()); err != nil {
		t.Fatalf("processBatch: %v", err)
	}
	if !row.failed || row.Attempts != 3 {
		t.Errorf("al 3.er intento debe marcarse fallido: failed=%v attempts=%d", row.failed, row.Attempts)
	}
}

func TestOutboxProcesaVariosLotes(t *testing.T) {
	store, mailer := newFakeOutbox(25), &fakeMailer{}
	w := NewEmailOutboxWorker(store, mailer, EmailOutboxWorkerConfig{BatchSize: 10, PollInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for {
		store.mu.Lock()
		n := len(mailer.sent)
		store.mu.Unlock()
		if n == 25 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("esperaba 25 envíos sin esperar al siguiente tick, obtuve %d", n)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run debe terminar al cancelar el contexto")
	}
}
