package services

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// EmailOutboxWorkerConfig ajusta el worker. Los valores en cero usan los
// defaults de NewEmailOutboxWorker.
type EmailOutboxWorkerConfig struct {
	PollInterval time.Duration // cada cuánto busca correos pendientes (default 5s)
	BatchSize    int32         // correos por lote (default 10)
	MaxAttempts  int32         // intentos antes de marcarlo como fallido (default 5)
	RetryBase    time.Duration // backoff tras un fallo: base·2^(intentos-1) (default 30s)
	SendTimeout  time.Duration // plazo de cada envío (default 15s)
}

// EmailOutboxWorker envía los correos de email_outbox. Cada lote se procesa
// en una transacción: ClaimDueEmails bloquea las filas con SKIP LOCKED, así
// que varias réplicas no envían el mismo correo. La entrega es "al menos una
// vez": si el proceso muere entre el envío y el commit, el correo se reenvía.
type EmailOutboxWorker struct {
	store  ports.OutboxStore
	mailer ports.Mailer
	cfg    EmailOutboxWorkerConfig
	now    func() time.Time
}

// NewEmailOutboxWorker crea el worker. Run debe lanzarse en una goroutine.
func NewEmailOutboxWorker(store ports.OutboxStore, mailer ports.Mailer, cfg EmailOutboxWorkerConfig) *EmailOutboxWorker {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = 30 * time.Second
	}
	if cfg.SendTimeout <= 0 {
		cfg.SendTimeout = 15 * time.Second
	}
	return &EmailOutboxWorker{store: store, mailer: mailer, cfg: cfg, now: time.Now}
}

// Run procesa el outbox hasta que ctx se cancela. Un lote ya empezado se
// termina aunque llegue la señal, para no dejar correos enviados sin marcar.
func (w *EmailOutboxWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		// Mientras salgan lotes completos puede haber más pendientes.
		for ctx.Err() == nil {
			n, err := w.processBatch(context.WithoutCancel(ctx))
			if err != nil {
				log.Printf("outbox de correo: %v", err)
				break
			}
			if n < int(w.cfg.BatchSize) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// processBatch envía un lote y devuelve cuántos correos tomó.
func (w *EmailOutboxWorker) processBatch(ctx context.Context) (int, error) {
	var claimed int
	err := w.store.WithTx(ctx, func(q ports.OutboxQueries) error {
		rows, err := q.ClaimDueEmails(ctx, w.cfg.BatchSize)
		if err != nil {
			return err
		}
		claimed = len(rows)
		for _, row := range rows {
			if err := w.deliver(ctx, q, row); err != nil {
				return err
			}
		}
		return nil
	})
	return claimed, err
}

// deliver envía un correo y registra el resultado. Solo devuelve error si
// falla la actualización de la fila, no el envío.
func (w *EmailOutboxWorker) deliver(ctx context.Context, q ports.OutboxQueries, row sqlc.ClaimDueEmailsRow) error {
	sendCtx, cancel := context.WithTimeout(ctx, w.cfg.SendTimeout)
	sendErr := w.mailer.Send(sendCtx, domain.Email{
		To:      row.ToAddress,
		Subject: row.Subject,
		Text:    row.TextBody,
		HTML:    row.HtmlBody,
	})
	cancel()
	if sendErr == nil {
		return q.MarkEmailSent(ctx, row.ID)
	}

	attempts := row.Attempts + 1
	lastErr := sql.NullString{String: sendErr.Error(), Valid: true}
	if attempts >= w.cfg.MaxAttempts {
		log.Printf("outbox de correo: %s descartado tras %d intentos: %v", row.ID, attempts, sendErr)
		return q.MarkEmailFailed(ctx, sqlc.MarkEmailFailedParams{ID: row.ID, LastError: lastErr})
	}
	next := w.now().Add(w.cfg.RetryBase << (attempts - 1))
	log.Printf("outbox de correo: %s falló (intento %d), reintento a las %s: %v", row.ID, attempts, next.Format(time.TimeOnly), sendErr)
	return q.MarkEmailRetry(ctx, sqlc.MarkEmailRetryParams{ID: row.ID, NextAttemptAt: next, LastError: lastErr})
}
