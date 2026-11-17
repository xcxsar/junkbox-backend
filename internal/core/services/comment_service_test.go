package services

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// fakeComments guarda los comentarios en memoria e implementa también
// CommentRemover con la misma regla que el adaptador.
type fakeComments struct {
	reviews  map[uuid.UUID]bool
	comments map[uuid.UUID]sqlc.GetCommentRow
	clock    time.Time
}

func newFakeComments() *fakeComments {
	return &fakeComments{reviews: map[uuid.UUID]bool{}, comments: map[uuid.UUID]sqlc.GetCommentRow{}, clock: time.Now()}
}

func (f *fakeComments) ReviewExists(_ context.Context, id uuid.UUID) (bool, error) {
	return f.reviews[id], nil
}

func (f *fakeComments) CreateComment(_ context.Context, arg sqlc.CreateCommentParams) (uuid.UUID, error) {
	id := uuid.New()
	f.clock = f.clock.Add(time.Second)
	f.comments[id] = sqlc.GetCommentRow{ID: id, ReviewID: arg.ReviewID, UserID: arg.UserID, ParentID: arg.ParentID, Content: arg.Content, CreatedAt: f.clock}
	return id, nil
}

func (f *fakeComments) replies(id uuid.UUID) int64 {
	var n int64
	for _, c := range f.comments {
		if c.ParentID != nil && *c.ParentID == id {
			n++
		}
	}
	return n
}

func (f *fakeComments) GetComment(_ context.Context, id uuid.UUID) (sqlc.GetCommentRow, error) {
	c, ok := f.comments[id]
	if !ok {
		return sqlc.GetCommentRow{}, sql.ErrNoRows
	}
	c.RepliesCount = f.replies(id)
	return c, nil
}

func (f *fakeComments) ListRootComments(_ context.Context, arg sqlc.ListRootCommentsParams) ([]sqlc.ListRootCommentsRow, error) {
	var out []sqlc.ListRootCommentsRow
	for _, c := range f.comments {
		if c.ReviewID == arg.ReviewID && c.ParentID == nil {
			c.RepliesCount = f.replies(c.ID)
			out = append(out, sqlc.ListRootCommentsRow(c))
		}
	}
	return out, nil
}

func (f *fakeComments) ListReplies(_ context.Context, arg sqlc.ListRepliesParams) ([]sqlc.ListRepliesRow, error) {
	var out []sqlc.ListRepliesRow
	for _, c := range f.comments {
		if c.ParentID != nil && *c.ParentID == *arg.ParentID {
			out = append(out, sqlc.ListRepliesRow(c))
		}
	}
	return out, nil
}

func (f *fakeComments) UpdateComment(_ context.Context, arg sqlc.UpdateCommentParams) (int64, error) {
	c, ok := f.comments[arg.ID]
	if !ok || c.DeletedAt.Valid {
		return 0, nil
	}
	c.Content = arg.Content
	f.comments[arg.ID] = c
	return 1, nil
}

func (f *fakeComments) RemoveComment(_ context.Context, id uuid.UUID) (bool, error) {
	c, ok := f.comments[id]
	if !ok {
		return false, domain.ErrNotFound
	}
	if c.ParentID == nil && f.replies(id) > 0 {
		c.Content, c.DeletedAt = sql.NullString{}, sql.NullTime{Time: time.Now(), Valid: true}
		f.comments[id] = c
		return true, nil
	}
	delete(f.comments, id)
	if c.ParentID != nil {
		if p := f.comments[*c.ParentID]; p.DeletedAt.Valid && f.replies(p.ID) == 0 {
			delete(f.comments, p.ID)
		}
	}
	return false, nil
}

func newCommentFixture() (*fakeComments, *CommentService, uuid.UUID) {
	store := newFakeComments()
	review := uuid.New()
	store.reviews[review] = true
	return store, NewCommentService(store, store), review
}

func TestCommentRespuestaAUnaRespuestaCuelgaDelRaiz(t *testing.T) {
	_, svc, review := newCommentFixture()
	ctx, user := context.Background(), uuid.New()

	root, err := svc.Create(ctx, user, review, nil, "  Gran reseña  ")
	if err != nil {
		t.Fatal(err)
	}
	if *root.Content != "Gran reseña" || root.ParentID != nil {
		t.Fatalf("raíz inesperado: %+v", root)
	}
	reply, err := svc.Create(ctx, uuid.New(), review, &root.ID, "De acuerdo")
	if err != nil {
		t.Fatal(err)
	}
	nested, err := svc.Create(ctx, user, review, &reply.ID, "Gracias")
	if err != nil {
		t.Fatal(err)
	}
	if nested.ParentID == nil || *nested.ParentID != root.ID {
		t.Fatalf("la respuesta a una respuesta debía colgar del raíz: %+v", nested.ParentID)
	}

	page, err := svc.List(ctx, review, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Comments) != 1 || page.Comments[0].RepliesCount != 2 {
		t.Fatalf("raíces: %+v", page.Comments)
	}
	replies, err := svc.Replies(ctx, root.ID, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies.Comments) != 1 || replies.NextCursor == nil || *replies.ParentID != root.ID {
		t.Fatalf("respuestas: %+v", replies)
	}
	var v *domain.ValidationError
	if _, err := svc.Replies(ctx, reply.ID, "", 0); !errors.As(err, &v) {
		t.Fatalf("pedir respuestas de una respuesta: esperaba ValidationError, obtuve %v", err)
	}
}

func TestCommentBorradoLogicoYFisico(t *testing.T) {
	store, svc, review := newCommentFixture()
	ctx, author, other := context.Background(), uuid.New(), uuid.New()
	root, _ := svc.Create(ctx, author, review, nil, "Raíz")
	reply, _ := svc.Create(ctx, other, review, &root.ID, "Respuesta")

	if err := svc.Delete(ctx, other, root.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("borrar ajeno: esperaba ErrForbidden, obtuve %v", err)
	}
	if err := svc.Delete(ctx, author, root.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.get(ctx, root.ID)
	if err != nil || !got.Deleted || got.Content != nil {
		t.Fatalf("el raíz con respuestas debía quedar como eliminado: %+v %v", got, err)
	}
	var v *domain.ValidationError
	if _, err := svc.Create(ctx, other, review, &root.ID, "Otra"); !errors.As(err, &v) {
		t.Fatalf("responder a un eliminado: esperaba ValidationError, obtuve %v", err)
	}
	if _, err := svc.Update(ctx, author, root.ID, "Edito"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("editar eliminado: esperaba ErrNotFound, obtuve %v", err)
	}
	if err := svc.Delete(ctx, other, reply.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.comments) != 0 {
		t.Fatalf("al borrar la última respuesta debía borrarse el raíz eliminado: %d comentarios", len(store.comments))
	}
}

func TestCommentValidaciones(t *testing.T) {
	store, svc, review := newCommentFixture()
	ctx, user := context.Background(), uuid.New()
	var v *domain.ValidationError
	for name, content := range map[string]string{"vacío": "   ", "largo": strings.Repeat("ñ", MaxCommentLen+1)} {
		if _, err := svc.Create(ctx, user, review, nil, content); !errors.As(err, &v) {
			t.Errorf("%s: esperaba ValidationError, obtuve %v", name, err)
		}
	}
	if _, err := svc.Create(ctx, user, uuid.New(), nil, "Hola"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("reseña inexistente: esperaba ErrNotFound, obtuve %v", err)
	}
	root, _ := svc.Create(ctx, user, review, nil, "Raíz")
	otherReview := uuid.New()
	store.reviews[otherReview] = true
	if _, err := svc.Create(ctx, user, otherReview, &root.ID, "Hola"); !errors.As(err, &v) {
		t.Errorf("padre de otra reseña: esperaba ValidationError, obtuve %v", err)
	}
	edited, err := svc.Update(ctx, user, root.ID, "Editado")
	if err != nil || *edited.Content != "Editado" {
		t.Fatalf("editar: %+v %v", edited, err)
	}
	if _, err := svc.Update(ctx, uuid.New(), root.ID, "Ajeno"); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("editar ajeno: esperaba ErrForbidden, obtuve %v", err)
	}
}
