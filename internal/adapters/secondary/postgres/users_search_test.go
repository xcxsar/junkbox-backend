package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// SEARCH-04: el orden, las tildes, la exclusión de quien busca y el escape
// de LIKE están en la consulta SearchUsers, así que se prueban contra
// PostgreSQL (se omite sin TEST_DATABASE_URL, ver catalog_importer_test.go).
func TestSearchUsersContraPostgres(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	q := sqlc.New(db)

	// Usernames únicos por ejecución para no chocar con usuarios reales.
	// Cada grupo lleva su propio sufijo aleatorio: si compartieran uno, la
	// coincidencia aproximada los relacionaría entre sí.
	tag := func() string { return strings.ReplaceAll(uuid.NewString()[:8], "-", "") }
	base, accTag, typoTag := "s04"+tag(), tag(), tag()
	type user struct {
		id       uuid.UUID
		username string
		display  string
	}
	mk := func(username, display string) user {
		return user{id: uuid.New(), username: username, display: display}
	}
	var (
		viewer     = mk(base+"_yo", "Quien busca")
		exact      = mk(base, "Exacto")
		prefix     = mk(base+"_dos", "")
		wordPrefix = mk("w"+tag(), "Ana "+base+"ez")
		accented   = mk("a"+accTag, "Lúcia Peñalver "+accTag)
		typo       = mk("m"+typoTag, "Montserrat "+typoTag)
	)
	all := []user{viewer, exact, prefix, wordPrefix, accented, typo}
	t.Cleanup(func() {
		for _, u := range all {
			exec(t, db, `DELETE FROM users WHERE id = $1`, u.id)
		}
	})
	for _, u := range all {
		exec(t, db, `INSERT INTO users (id, username, email, hashed_password, display_name) VALUES ($1, $2, $3, 'x', NULLIF($4, ''))`,
			u.id, u.username, u.username+"@example.invalid", u.display)
	}
	exec(t, db, `INSERT INTO follows (follower_id, followee_id) VALUES ($1, $2)`, viewer.id, prefix.id)

	search := func(query string) []sqlc.SearchUsersRow {
		t.Helper()
		rows, err := q.SearchUsers(ctx, sqlc.SearchUsersParams{ViewerID: viewer.id, Query: query, MaxResults: 50})
		if err != nil {
			t.Fatalf("SearchUsers(%q): %v", query, err)
		}
		return rows
	}
	usernames := func(rows []sqlc.SearchUsersRow) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.Username
		}
		return out
	}

	t.Run("orden: exacto, prefijo de username, prefijo de palabra; sin quien busca", func(t *testing.T) {
		rows := search(strings.ToUpper(base)) // tampoco distingue mayúsculas
		got := usernames(rows)
		want := []string{exact.username, prefix.username, wordPrefix.username}
		if len(got) != len(want) {
			t.Fatalf("usuarios = %v, se esperaba %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("usuarios = %v, se esperaba %v", got, want)
			}
		}
		if rows[0].FollowedByMe || !rows[1].FollowedByMe {
			t.Fatalf("followed_by_me: solo debía seguir a %s: %+v", prefix.username, rows)
		}
		if !rows[0].DisplayName.Valid || rows[0].DisplayName.String != "Exacto" || rows[1].DisplayName != (sql.NullString{}) {
			t.Fatalf("display_name mal leído: %+v", rows)
		}
	})

	t.Run("sin tildes", func(t *testing.T) {
		if got := usernames(search("penalver " + accTag)); len(got) != 1 || got[0] != accented.username {
			t.Fatalf(`"penalver" debía encontrar a "Peñalver": %v`, got)
		}
		if got := usernames(search("LUCIA")); !contains(got, accented.username) {
			t.Fatalf(`"LUCIA" debía encontrar a "Lúcia": %v`, got)
		}
	})

	t.Run("tolera erratas", func(t *testing.T) {
		if got := usernames(search("montserat " + typoTag)); !contains(got, typo.username) {
			t.Fatalf(`"montserat" debía encontrar a "Montserrat": %v`, got)
		}
	})

	t.Run("comodines de LIKE como texto", func(t *testing.T) {
		// Sin escapar, "%" o "_" coincidirían con todos los usuarios.
		for _, query := range []string{"%", "_", `\`} {
			if got := search(query); len(got) != 0 {
				t.Fatalf("%q no debía coincidir con nadie: %v", query, usernames(got))
			}
		}
	})
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
