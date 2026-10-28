package services

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// --- fakes ---

// fakeUserStore simula la tabla users en memoria.
type fakeUserStore struct {
	users       map[uuid.UUID]sqlc.User
	createErr   error // error forzado en CreateUser (p. ej. carrera 23505)
	existsCalls int
}

func newFakeUserStore(seed ...sqlc.User) *fakeUserStore {
	f := &fakeUserStore{users: map[uuid.UUID]sqlc.User{}}
	for _, u := range seed {
		f.users[u.ID] = u
	}
	return f
}

func (f *fakeUserStore) CreateUser(_ context.Context, arg sqlc.CreateUserParams) (sqlc.CreateUserRow, error) {
	if f.createErr != nil {
		return sqlc.CreateUserRow{}, f.createErr
	}
	u := sqlc.User{
		ID:             uuid.New(),
		Username:       arg.Username,
		Email:          arg.Email,
		HashedPassword: arg.HashedPassword,
		DisplayName:    arg.DisplayName,
		AvatarUrl:      arg.AvatarUrl,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	f.users[u.ID] = u
	return sqlc.CreateUserRow{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		AvatarUrl:   u.AvatarUrl,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}, nil
}

func (f *fakeUserStore) find(match func(sqlc.User) bool) (sqlc.User, error) {
	for _, u := range f.users {
		if match(u) {
			return u, nil
		}
	}
	return sqlc.User{}, sql.ErrNoRows
}

func (f *fakeUserStore) GetUserByEmail(_ context.Context, email string) (sqlc.User, error) {
	return f.find(func(u sqlc.User) bool { return u.Email == email })
}

func (f *fakeUserStore) GetUserByUsername(_ context.Context, username string) (sqlc.User, error) {
	return f.find(func(u sqlc.User) bool { return u.Username == username })
}

func (f *fakeUserStore) GetUserByID(_ context.Context, id uuid.UUID) (sqlc.User, error) {
	return f.find(func(u sqlc.User) bool { return u.ID == id })
}

func (f *fakeUserStore) ExistsUserByEmailOrUsername(_ context.Context, arg sqlc.ExistsUserByEmailOrUsernameParams) (bool, error) {
	f.existsCalls++
	_, err := f.find(func(u sqlc.User) bool { return u.Email == arg.Email || u.Username == arg.Username })
	return err == nil, nil
}

// fakeTokenStore guarda tokens en memoria y simula la tabla refresh_tokens.
type fakeTokenStore struct {
	byHash        map[string]sqlc.RefreshToken
	families      map[uuid.UUID]bool // familias revocadas
	forceZeroRows bool               // simula la carrera: RevokeRefreshToken afecta 0 filas
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{
		byHash:   map[string]sqlc.RefreshToken{},
		families: map[uuid.UUID]bool{},
	}
}

func (f *fakeTokenStore) InsertRefreshToken(_ context.Context, arg sqlc.InsertRefreshTokenParams) (sqlc.RefreshToken, error) {
	row := sqlc.RefreshToken{
		ID:        uuid.New(),
		UserID:    arg.UserID,
		TokenHash: arg.TokenHash,
		FamilyID:  arg.FamilyID,
		ExpiresAt: arg.ExpiresAt,
	}
	f.byHash[arg.TokenHash] = row
	return row, nil
}

func (f *fakeTokenStore) GetRefreshTokenByHash(_ context.Context, hash string) (sqlc.RefreshToken, error) {
	row, ok := f.byHash[hash]
	if !ok {
		return sqlc.RefreshToken{}, sql.ErrNoRows
	}
	return row, nil
}

func (f *fakeTokenStore) RevokeRefreshToken(_ context.Context, hash string) (int64, error) {
	if f.forceZeroRows {
		// Carrera simulada: otra petición ya había rotado este token.
		return 0, nil
	}
	row, ok := f.byHash[hash]
	if !ok || row.Revoked {
		return 0, nil
	}
	row.Revoked = true
	row.RevokedAt = sql.NullTime{Time: time.Now(), Valid: true}
	f.byHash[hash] = row
	return 1, nil
}

func (f *fakeTokenStore) SetRefreshTokenReplacedBy(_ context.Context, arg sqlc.SetRefreshTokenReplacedByParams) error {
	for h, r := range f.byHash {
		if r.ID == arg.ID {
			r.ReplacedBy = arg.ReplacedBy
			f.byHash[h] = r
		}
	}
	return nil
}

func (f *fakeTokenStore) RevokeRefreshTokenFamily(_ context.Context, familyID uuid.UUID) error {
	f.families[familyID] = true
	for h, r := range f.byHash {
		if r.FamilyID == familyID {
			r.Revoked = true
			r.RevokedAt = sql.NullTime{Time: time.Now(), Valid: true}
			f.byHash[h] = r
		}
	}
	return nil
}

func (f *fakeTokenStore) RevokeAllRefreshTokensByUser(_ context.Context, userID uuid.UUID) error {
	for h, r := range f.byHash {
		if r.UserID == userID {
			r.Revoked = true
			r.RevokedAt = sql.NullTime{Time: time.Now(), Valid: true}
			f.byHash[h] = r
		}
	}
	return nil
}

// fakeRevocationStore registra las revocaciones de access tokens.
type fakeRevocationStore struct {
	revokedJTI       map[uuid.UUID]bool
	allRevokedBefore map[uuid.UUID]time.Time
}

func newFakeRevocationStore() *fakeRevocationStore {
	return &fakeRevocationStore{
		revokedJTI:       map[uuid.UUID]bool{},
		allRevokedBefore: map[uuid.UUID]time.Time{},
	}
}

func (f *fakeRevocationStore) RevokeAccessToken(_ context.Context, arg sqlc.RevokeAccessTokenParams) error {
	f.revokedJTI[arg.Jti] = true
	return nil
}

func (f *fakeRevocationStore) IsAccessTokenRevoked(_ context.Context, jti uuid.UUID) (bool, error) {
	return f.revokedJTI[jti], nil
}

func (f *fakeRevocationStore) RevokeAllUserAccessTokens(_ context.Context, userID uuid.UUID) error {
	f.allRevokedBefore[userID] = time.Now()
	return nil
}

func (f *fakeRevocationStore) GetUserAccessRevocation(_ context.Context, userID uuid.UUID) (time.Time, error) {
	t, ok := f.allRevokedBefore[userID]
	if !ok {
		return time.Time{}, sql.ErrNoRows
	}
	return t, nil
}

// fakeHasher evita el coste de bcrypt en los tests.
type fakeHasher struct{}

func (fakeHasher) Hash(plain []byte) (string, error) { return "hash:" + string(plain), nil }
func (fakeHasher) Compare(hash string, plain []byte) error {
	if hash != "hash:"+string(plain) {
		return errors.New("mismatch")
	}
	return nil
}

type fakeJWT struct{ ttl time.Duration }

func (f *fakeJWT) GenerateAccess(userID uuid.UUID, _ string) (string, error) {
	return "access." + userID.String(), nil
}
func (f *fakeJWT) ParseAccess(string) (*domain.AccessClaims, error) { return nil, errors.New("no usado") }
func (f *fakeJWT) TTL() time.Duration                               { return f.ttl }

// uniqueViolation imita a *pgconn.PgError con SQLSTATE 23505.
type uniqueViolation struct{}

func (uniqueViolation) Error() string    { return "duplicate key value violates unique constraint" }
func (uniqueViolation) SQLState() string { return "23505" }

// --- helpers ---

var seedUser = sqlc.User{
	ID:             uuid.MustParse("b1000000-0000-0000-0000-000000000001"),
	Username:       "cesar_beltran",
	Email:          "cesar@junkbox.local",
	HashedPassword: "hash:Password123!",
	DisplayName:    sql.NullString{String: "César", Valid: true},
	CreatedAt:      time.Now(),
}

type testEnv struct {
	svc    *AuthService
	users  *fakeUserStore
	tokens *fakeTokenStore
	rev    *fakeRevocationStore
}

func newEnv() *testEnv {
	env := &testEnv{
		users:  newFakeUserStore(seedUser),
		tokens: newFakeTokenStore(),
		rev:    newFakeRevocationStore(),
	}
	env.svc = NewAuthService(env.users, env.tokens, env.rev, fakeHasher{}, &fakeJWT{ttl: 15 * time.Minute}, 7*24*time.Hour)
	return env
}

func mustLogin(t *testing.T, env *testEnv) *domain.TokenPair {
	t.Helper()
	pair, _, err := env.svc.Login(context.Background(), "cesar_beltran", "Password123!")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	return pair
}

// testClaims devuelve claims con jti y exp como los que produce el JWT real.
func testClaims() *domain.AccessClaims {
	return &domain.AccessClaims{
		UserID:    seedUser.ID,
		Username:  seedUser.Username,
		JTI:       uuid.NewString(),
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}
}

func validRegister() RegisterInput {
	return RegisterInput{
		Username:    "Melomano_Pro",
		Email:       " Melomano@Junkbox.local ",
		Password:    "Password123!",
		DisplayName: "Crítico Musical",
		AvatarURL:   "https://api.dicebear.com/7.x/bottts/svg?seed=melomano",
	}
}

// --- register ---

func TestRegisterOK(t *testing.T) {
	env := newEnv()
	pair, profile, err := env.svc.Register(context.Background(), validRegister())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" || pair.ExpiresInSec != 900 {
		t.Fatalf("pair incompleto: %+v", pair)
	}
	if profile.Username != "melomano_pro" || profile.Email != "melomano@junkbox.local" {
		t.Errorf("username/email deben normalizarse a minúsculas: %+v", profile)
	}
	if profile.DisplayName == nil || *profile.DisplayName != "Crítico Musical" {
		t.Errorf("display_name inesperado: %v", profile.DisplayName)
	}
	stored, _ := env.users.GetUserByID(context.Background(), profile.ID)
	if stored.HashedPassword != "hash:Password123!" {
		t.Error("la contraseña debe guardarse hasheada")
	}
	if len(env.tokens.byHash) != 1 {
		t.Errorf("esperaba 1 refresh persistido, obtuve %d", len(env.tokens.byHash))
	}
}

func TestRegisterOpcionalesVacios(t *testing.T) {
	env := newEnv()
	in := validRegister()
	in.DisplayName, in.AvatarURL = "", ""
	_, profile, err := env.svc.Register(context.Background(), in)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if profile.DisplayName != nil || profile.AvatarURL != nil {
		t.Errorf("display_name y avatar_url vacíos deben quedar null: %+v", profile)
	}
}

func TestRegisterDuplicado(t *testing.T) {
	env := newEnv()
	in := validRegister()
	in.Username = "CESAR_BELTRAN"
	_, _, err := env.svc.Register(context.Background(), in)
	if !errors.Is(err, domain.ErrUserAlreadyExists) {
		t.Fatalf("esperaba ErrUserAlreadyExists, obtuve %v", err)
	}
}

func TestRegisterCarreraUniqueViolation(t *testing.T) {
	env := newEnv()
	env.users.createErr = uniqueViolation{}
	_, _, err := env.svc.Register(context.Background(), validRegister())
	if !errors.Is(err, domain.ErrUserAlreadyExists) {
		t.Fatalf("esperaba ErrUserAlreadyExists ante 23505, obtuve %v", err)
	}
}

func TestRegisterValidaciones(t *testing.T) {
	cases := map[string]func(*RegisterInput){
		"username corto":       func(in *RegisterInput) { in.Username = "ab" },
		"username con espacio": func(in *RegisterInput) { in.Username = "mel omano" },
		"email inválido":       func(in *RegisterInput) { in.Email = "no-es-email" },
		"email con nombre":     func(in *RegisterInput) { in.Email = "Mel <mel@x.mx>" },
		"password corta":       func(in *RegisterInput) { in.Password = "corta" },
		"password > 72 bytes":  func(in *RegisterInput) { in.Password = string(make([]byte, 73)) },
		"avatar no http":       func(in *RegisterInput) { in.AvatarURL = "javascript:alert(1)" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			env := newEnv()
			in := validRegister()
			mutate(&in)
			_, _, err := env.svc.Register(context.Background(), in)
			var verr *domain.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("esperaba ValidationError, obtuve %v", err)
			}
			if env.users.existsCalls != 0 {
				t.Error("no debe consultarse la BD si la validación falla")
			}
		})
	}
}

// --- login ---

func TestLoginPorUsernameYEmail(t *testing.T) {
	for _, id := range []string{"cesar_beltran", "CESAR@junkbox.local"} {
		env := newEnv()
		pair, profile, err := env.svc.Login(context.Background(), id, "Password123!")
		if err != nil {
			t.Fatalf("Login(%q): %v", id, err)
		}
		if pair.ExpiresInSec != 900 || profile.ID != seedUser.ID {
			t.Errorf("Login(%q) incompleto: %+v %+v", id, pair, profile)
		}
	}
}

func TestLoginUsuarioInexistente(t *testing.T) {
	env := newEnv()
	_, _, err := env.svc.Login(context.Background(), "nadie", "Password123!")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("esperaba ErrInvalidCredentials, obtuve %v", err)
	}
}

func TestLoginPasswordMala(t *testing.T) {
	env := newEnv()
	_, _, err := env.svc.Login(context.Background(), "cesar_beltran", "wrong")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("esperaba ErrInvalidCredentials, obtuve %v", err)
	}
}

// --- refresh ---

func TestRefreshRotacion(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)

	newPair, err := env.svc.Refresh(context.Background(), pair.RefreshToken, testClaims())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if newPair.RefreshToken == pair.RefreshToken {
		t.Error("el refresh token debe rotarse (valor distinto)")
	}
	old := env.tokens.byHash[hashToken(pair.RefreshToken)]
	if !old.Revoked {
		t.Error("el token viejo debe quedar revocado")
	}
	if old.ReplacedBy == nil {
		t.Error("el token viejo debe enlazarse con replaced_by")
	}
	if fresh := env.tokens.byHash[hashToken(newPair.RefreshToken)]; fresh.FamilyID != old.FamilyID {
		t.Error("el token nuevo debe conservar la familia")
	}
}

func TestRefreshReusoRevocaFamilia(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)

	if _, err := env.svc.Refresh(context.Background(), pair.RefreshToken, testClaims()); err != nil {
		t.Fatalf("primer refresh: %v", err)
	}
	// Reintento con el token viejo (posible robo).
	_, err := env.svc.Refresh(context.Background(), pair.RefreshToken, testClaims())
	if !errors.Is(err, domain.ErrRefreshReuse) {
		t.Fatalf("esperaba ErrRefreshReuse, obtuve %v", err)
	}
	for _, tok := range env.tokens.byHash {
		if !tok.Revoked {
			t.Error("todos los tokens de la familia deben quedar revocados")
		}
	}
}

func TestRefreshCarreraRevocaFamilia(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)

	// Entre la lectura del token y su revocación, otra petición lo rotó.
	env.tokens.forceZeroRows = true
	_, err := env.svc.Refresh(context.Background(), pair.RefreshToken, testClaims())
	if !errors.Is(err, domain.ErrRefreshReuse) {
		t.Fatalf("esperaba ErrRefreshReuse por carrera, obtuve %v", err)
	}
	if !env.tokens.families[env.tokens.byHash[hashToken(pair.RefreshToken)].FamilyID] {
		t.Error("la familia debe quedar revocada ante la carrera")
	}
}

func TestRefreshTokenInexistente(t *testing.T) {
	env := newEnv()
	_, err := env.svc.Refresh(context.Background(), "basura", testClaims())
	if !errors.Is(err, domain.ErrInvalidRefresh) {
		t.Errorf("esperaba ErrInvalidRefresh, obtuve %v", err)
	}
}

func TestRefreshExpirado(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)

	h := hashToken(pair.RefreshToken)
	tok := env.tokens.byHash[h]
	tok.ExpiresAt = time.Now().Add(-time.Minute)
	env.tokens.byHash[h] = tok

	if _, err := env.svc.Refresh(context.Background(), pair.RefreshToken, testClaims()); !errors.Is(err, domain.ErrInvalidRefresh) {
		t.Errorf("esperaba ErrInvalidRefresh con token expirado, obtuve %v", err)
	}
}

func TestRefreshRevocaAccessAnterior(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)
	old := testClaims()

	if _, err := env.svc.Refresh(context.Background(), pair.RefreshToken, old); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !env.rev.revokedJTI[uuid.MustParse(old.JTI)] {
		t.Error("el access token anterior debe quedar en la denylist")
	}
}

func TestRefreshAccessExpiradoNoVaALaDenylist(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)
	old := testClaims()
	old.ExpiresAt = time.Now().Add(-time.Minute)

	if _, err := env.svc.Refresh(context.Background(), pair.RefreshToken, old); err != nil {
		t.Fatalf("Refresh con access expirado: %v", err)
	}
	if len(env.rev.revokedJTI) != 0 {
		t.Error("un access ya expirado no necesita denylist")
	}
}

func TestRefreshSinAccessAnterior(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)

	if _, err := env.svc.Refresh(context.Background(), pair.RefreshToken, nil); !errors.Is(err, domain.ErrInvalidRefresh) {
		t.Fatalf("esperaba ErrInvalidRefresh sin access, obtuve %v", err)
	}
	if env.tokens.byHash[hashToken(pair.RefreshToken)].Revoked {
		t.Error("sin access no debe gastarse el refresh token")
	}
}

func TestRefreshAccessDeOtroUsuario(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)
	other := testClaims()
	other.UserID = uuid.New()

	if _, err := env.svc.Refresh(context.Background(), pair.RefreshToken, other); !errors.Is(err, domain.ErrInvalidRefresh) {
		t.Fatalf("esperaba ErrInvalidRefresh, obtuve %v", err)
	}
	if env.tokens.byHash[hashToken(pair.RefreshToken)].Revoked {
		t.Error("con un access ajeno no debe gastarse el refresh token")
	}
	if len(env.rev.revokedJTI) != 0 {
		t.Error("no debe revocarse el access de otro usuario")
	}
}

// --- logout ---

func TestLogoutIdempotente(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)

	if err := env.svc.Logout(context.Background(), testClaims(), pair.RefreshToken, false); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if err := env.svc.Logout(context.Background(), testClaims(), pair.RefreshToken, false); err != nil {
		t.Fatalf("Logout idempotente: %v", err)
	}
	if !env.tokens.byHash[hashToken(pair.RefreshToken)].Revoked {
		t.Error("logout debe revocar el refresh token")
	}
}

func TestLogoutRevocaAccessToken(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)

	claims := testClaims()
	if err := env.svc.Logout(context.Background(), claims, pair.RefreshToken, false); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !env.rev.revokedJTI[uuid.MustParse(claims.JTI)] {
		t.Error("logout debe revocar el jti del access token")
	}
	if len(env.rev.allRevokedBefore) != 0 {
		t.Error("sin all_sessions no debe haber corte global")
	}
}

func TestLogoutAllSessions(t *testing.T) {
	env := newEnv()
	pair := mustLogin(t, env)
	other := mustLogin(t, env) // segunda sesión del mismo usuario

	claims := testClaims()
	if err := env.svc.Logout(context.Background(), claims, pair.RefreshToken, true); err != nil {
		t.Fatalf("Logout all_sessions: %v", err)
	}
	if _, ok := env.rev.allRevokedBefore[claims.UserID]; !ok {
		t.Error("all_sessions debe registrar el corte global del usuario")
	}
	if !env.tokens.byHash[hashToken(other.RefreshToken)].Revoked {
		t.Error("all_sessions debe revocar todos los refresh tokens del usuario")
	}
}

// --- me ---

func TestMe(t *testing.T) {
	env := newEnv()
	profile, err := env.svc.Me(context.Background(), testClaims())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if profile.Username != "cesar_beltran" || profile.DisplayName == nil || profile.AvatarURL != nil {
		t.Errorf("Me inesperado: %+v", profile)
	}
}

func TestMeUsuarioBorrado(t *testing.T) {
	env := newEnv()
	claims := testClaims()
	claims.UserID = uuid.New()
	if _, err := env.svc.Me(context.Background(), claims); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Errorf("esperaba ErrInvalidCredentials, obtuve %v", err)
	}
}
