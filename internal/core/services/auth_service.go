// Package services implementa los casos de uso del núcleo de negocio.
package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/xcxsar/junkbox-backend/internal/core/domain"
	"github.com/xcxsar/junkbox-backend/internal/core/ports"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

// Límites de validación del registro (alineados con el esquema de users).
const (
	minPasswordBytes   = 8
	maxPasswordBytes   = 72 // bcrypt ignora lo que pasa de 72 bytes
	maxEmailLen        = 255
	maxDisplayNameLen  = 100
	maxAvatarURLLength = 2048
)

// usernamePattern: handle (@username) en minúsculas, 3 a 50 caracteres.
var usernamePattern = regexp.MustCompile(`^[a-z0-9_.]{3,50}$`)

// dummyHash es un hash bcrypt (coste 12) válido contra el que se compara la
// contraseña cuando el usuario no existe, para que login tarde lo mismo en
// ambos casos y no revele qué usuarios están registrados.
const dummyHash = "$2a$12$DwPSk67AN5gwogfqGQ7LPOtig2kS80tsp4.w5eP6ERF30881lLJ.G"

// AuthService implementa registro, login, rotación de refresh tokens,
// logout y perfil.
type AuthService struct {
	users      ports.UserStore
	tokens     ports.TokenStore
	revocation ports.RevocationStore
	hasher     ports.PasswordHasher
	jwt        ports.TokenManager
	refreshTTL time.Duration
}

// NewAuthService crea el servicio. refreshTTL es la vigencia de los
// refresh tokens (típicamente 168h, desde config).
func NewAuthService(users ports.UserStore, tokens ports.TokenStore, revocation ports.RevocationStore, hasher ports.PasswordHasher, jwt ports.TokenManager, refreshTTL time.Duration) *AuthService {
	return &AuthService{users: users, tokens: tokens, revocation: revocation, hasher: hasher, jwt: jwt, refreshTTL: refreshTTL}
}

// RegisterInput son los datos de alta de un usuario. DisplayName y
// AvatarURL son opcionales (cadena vacía = sin valor).
type RegisterInput struct {
	Username    string
	Email       string
	Password    string
	DisplayName string
	AvatarURL   string
}

// Register valida los datos, crea el usuario y emite el par de tokens inicial.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (*domain.TokenPair, *domain.UserProfile, error) {
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.AvatarURL = strings.TrimSpace(in.AvatarURL)
	if err := validateRegister(in); err != nil {
		return nil, nil, err
	}

	exists, err := s.users.ExistsUserByEmailOrUsername(ctx, sqlc.ExistsUserByEmailOrUsernameParams{
		Email:    in.Email,
		Username: in.Username,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("auth: comprobar unicidad: %w", err)
	}
	if exists {
		return nil, nil, domain.ErrUserAlreadyExists
	}

	hash, err := s.hasher.Hash([]byte(in.Password))
	if err != nil {
		return nil, nil, fmt.Errorf("auth: hashear contraseña: %w", err)
	}
	created, err := s.users.CreateUser(ctx, sqlc.CreateUserParams{
		Username:       in.Username,
		Email:          in.Email,
		HashedPassword: hash,
		DisplayName:    nullString(in.DisplayName),
		AvatarUrl:      nullString(in.AvatarURL),
	})
	if err != nil {
		// Carrera: otro registro con el mismo email/username entró entre
		// la comprobación de unicidad y el INSERT.
		if isUniqueViolation(err) {
			return nil, nil, domain.ErrUserAlreadyExists
		}
		return nil, nil, fmt.Errorf("auth: crear usuario: %w", err)
	}

	user := sqlc.User{
		ID:          created.ID,
		Username:    created.Username,
		Email:       created.Email,
		DisplayName: created.DisplayName,
		AvatarUrl:   created.AvatarUrl,
		CreatedAt:   created.CreatedAt,
		UpdatedAt:   created.UpdatedAt,
	}
	pair, err := s.issuePair(ctx, user, uuid.New() /* family nueva */)
	if err != nil {
		return nil, nil, err
	}
	profile := profileFromUser(user)
	return &pair.TokenPair, &profile, nil
}

// Login valida credenciales y emite el par de tokens inicial. identifier
// puede ser el email (si contiene "@") o el username.
func (s *AuthService) Login(ctx context.Context, identifier, password string) (*domain.TokenPair, *domain.UserProfile, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))

	var (
		user sqlc.User
		err  error
	)
	if strings.Contains(identifier, "@") {
		user, err = s.users.GetUserByEmail(ctx, identifier)
	} else {
		user, err = s.users.GetUserByUsername(ctx, identifier)
	}
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("auth: buscar usuario: %w", err)
		}
		// Usuario inexistente: comparar igualmente para no filtrar por tiempo.
		_ = s.hasher.Compare(dummyHash, []byte(password))
		return nil, nil, domain.ErrInvalidCredentials
	}
	if err := s.hasher.Compare(user.HashedPassword, []byte(password)); err != nil {
		return nil, nil, domain.ErrInvalidCredentials
	}

	pair, err := s.issuePair(ctx, user, uuid.New() /* family nueva */)
	if err != nil {
		return nil, nil, err
	}
	profile := profileFromUser(user)
	return &pair.TokenPair, &profile, nil
}

// Refresh rota un refresh token vigente a cambio del access token anterior
// (claims, con firma verificada aunque haya expirado): no se entrega una
// llave nueva sin recoger la vieja, que queda revocada en la denylist. Si
// el refresh token ya estaba revocado (posible robo), revoca la familia
// completa y devuelve ErrRefreshReuse.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string, claims *domain.AccessClaims) (*domain.TokenPair, error) {
	if claims == nil {
		return nil, domain.ErrInvalidRefresh
	}
	stored, err := s.tokens.GetRefreshTokenByHash(ctx, hashToken(refreshToken))
	if err != nil {
		return nil, domain.ErrInvalidRefresh
	}
	if claims.UserID != stored.UserID {
		// El access entregado no es de este usuario: no se gasta el refresh.
		return nil, domain.ErrInvalidRefresh
	}
	if stored.Revoked {
		// Reutilización de un token ya rotado/revocado: señal de robo.
		if err := s.tokens.RevokeRefreshTokenFamily(ctx, stored.FamilyID); err != nil {
			return nil, fmt.Errorf("auth: revocar familia: %w", err)
		}
		return nil, domain.ErrRefreshReuse
	}
	if !stored.ExpiresAt.After(time.Now()) {
		return nil, domain.ErrInvalidRefresh
	}

	rows, err := s.tokens.RevokeRefreshToken(ctx, hashToken(refreshToken))
	if err != nil {
		return nil, fmt.Errorf("auth: revocar refresh: %w", err)
	}
	if rows == 0 {
		// Carrera: otra petición rotó este mismo token entre la lectura y
		// la revocación. Tratar como reutilización y abortar la familia.
		if err := s.tokens.RevokeRefreshTokenFamily(ctx, stored.FamilyID); err != nil {
			return nil, fmt.Errorf("auth: revocar familia: %w", err)
		}
		return nil, domain.ErrRefreshReuse
	}

	// Recoger la llave vieja. Si ya expiró no hace falta la denylist.
	if claims.ExpiresAt.After(time.Now()) {
		if err := s.revokeCurrentAccess(ctx, claims); err != nil {
			return nil, err
		}
	}

	// Los claims se releen del usuario: el username del access token nuevo
	// refleja el estado actual, no el del login.
	user, err := s.users.GetUserByID(ctx, stored.UserID)
	if err != nil {
		return nil, fmt.Errorf("auth: usuario del refresh token inexistente: %w", err)
	}
	pair, err := s.issuePair(ctx, user, stored.FamilyID)
	if err != nil {
		return nil, err
	}
	// Enlazar el token nuevo con el anterior (audit trail en replaced_by).
	if err := s.tokens.SetRefreshTokenReplacedBy(ctx, sqlc.SetRefreshTokenReplacedByParams{
		ID:         stored.ID,
		ReplacedBy: &pair.refreshID,
	}); err != nil {
		return nil, fmt.Errorf("auth: enlazar replaced_by: %w", err)
	}
	return &pair.TokenPair, nil
}

// Logout cierra la sesión de verdad:
//  1. Revoca el refresh token enviado (idempotente).
//  2. Revoca el access token de la petición insertando su jti en la
//     denylist (revoked_access_tokens), con su exp para poder purgarlo.
//  3. Si allSessions es true, marca un corte global del usuario
//     (user_access_revocations) y revoca todos sus refresh tokens: quedan
//     invalidadas todas las sesiones previas.
//
// Repetir el logout no falla.
func (s *AuthService) Logout(ctx context.Context, claims *domain.AccessClaims, refreshToken string, allSessions bool) error {
	if _, err := s.tokens.RevokeRefreshToken(ctx, hashToken(refreshToken)); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	if err := s.revokeCurrentAccess(ctx, claims); err != nil {
		return err
	}
	if allSessions {
		if err := s.revocation.RevokeAllUserAccessTokens(ctx, claims.UserID); err != nil {
			return fmt.Errorf("auth: corte global de access tokens: %w", err)
		}
		if err := s.tokens.RevokeAllRefreshTokensByUser(ctx, claims.UserID); err != nil {
			return fmt.Errorf("auth: revocar refresh tokens del usuario: %w", err)
		}
	}
	return nil
}

// revokeCurrentAccess mete el jti del access token actual en la denylist.
func (s *AuthService) revokeCurrentAccess(ctx context.Context, claims *domain.AccessClaims) error {
	jti, err := uuid.Parse(claims.JTI)
	if err != nil {
		return fmt.Errorf("auth: jti inválido %q: %w", claims.JTI, err)
	}
	expires := claims.ExpiresAt
	if expires.IsZero() {
		expires = time.Now().Add(s.jwt.TTL())
	}
	if err := s.revocation.RevokeAccessToken(ctx, sqlc.RevokeAccessTokenParams{
		Jti:       jti,
		UserID:    claims.UserID,
		ExpiresAt: expires,
	}); err != nil {
		return fmt.Errorf("auth: revocar access token: %w", err)
	}
	return nil
}

// Me retorna el perfil del usuario autenticado.
func (s *AuthService) Me(ctx context.Context, claims *domain.AccessClaims) (*domain.UserProfile, error) {
	user, err := s.users.GetUserByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Usuario borrado con un access token aún vigente.
			return nil, domain.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: buscar usuario: %w", err)
	}
	profile := profileFromUser(user)
	return &profile, nil
}

// issuedPair incluye el id interno del refresh token para el enlace
// replaced_by (no se expone al cliente).
type issuedPair struct {
	domain.TokenPair
	refreshID uuid.UUID
}

// issuePair genera un refresh token opaco, lo persiste hash-eado con la
// familia dada y emite el access token con los claims del usuario.
func (s *AuthService) issuePair(ctx context.Context, user sqlc.User, familyID uuid.UUID) (*issuedPair, error) {
	refresh, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	stored, err := s.tokens.InsertRefreshToken(ctx, sqlc.InsertRefreshTokenParams{
		UserID:    user.ID,
		TokenHash: hashToken(refresh),
		FamilyID:  familyID,
		ExpiresAt: time.Now().Add(s.refreshTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("auth: persistir refresh token: %w", err)
	}

	access, err := s.jwt.GenerateAccess(user.ID, user.Username)
	if err != nil {
		return nil, err
	}
	return &issuedPair{
		TokenPair: domain.TokenPair{
			AccessToken:  access,
			RefreshToken: refresh,
			ExpiresInSec: int64(s.jwt.TTL().Seconds()),
		},
		refreshID: stored.ID,
	}, nil
}

func validateRegister(in RegisterInput) error {
	if !usernamePattern.MatchString(in.Username) {
		return domain.NewValidationError("username inválido: 3 a 50 caracteres (letras, números, '_' o '.')")
	}
	if len(in.Email) > maxEmailLen {
		return domain.NewValidationError("email inválido")
	}
	if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
		return domain.NewValidationError("email inválido")
	}
	if len(in.Password) < minPasswordBytes || len(in.Password) > maxPasswordBytes {
		return domain.NewValidationError(fmt.Sprintf("la contraseña debe tener entre %d y %d bytes", minPasswordBytes, maxPasswordBytes))
	}
	if utf8.RuneCountInString(in.DisplayName) > maxDisplayNameLen {
		return domain.NewValidationError(fmt.Sprintf("display_name no puede superar %d caracteres", maxDisplayNameLen))
	}
	if in.AvatarURL != "" {
		u, err := url.Parse(in.AvatarURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(in.AvatarURL) > maxAvatarURLLength {
			return domain.NewValidationError("avatar_url debe ser una URL http(s) válida")
		}
	}
	return nil
}

// isUniqueViolation detecta el SQLSTATE 23505 (unique_violation) sin
// acoplar el núcleo al driver: *pgconn.PgError expone SQLState().
func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}

// newRefreshToken genera un token opaco de 64 bytes aleatorios en base64url.
func newRefreshToken() (string, error) {
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generar refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken devuelve el hash SHA-256 en hex (64 chars) del token opaco.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func stringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

func profileFromUser(u sqlc.User) domain.UserProfile {
	return domain.UserProfile{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: stringPtr(u.DisplayName),
		AvatarURL:   stringPtr(u.AvatarUrl),
		CreatedAt:   u.CreatedAt,
	}
}
