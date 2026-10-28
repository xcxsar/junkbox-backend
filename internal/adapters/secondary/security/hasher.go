package security

import "golang.org/x/crypto/bcrypt"

// DefaultBcryptCost es el coste usado para los hashes de contraseñas
// (el mismo que los hashes del seed).
const DefaultBcryptCost = 12

// Hasher encapsula el hashing de contraseñas con bcrypt.
type Hasher struct {
	cost int
}

// NewHasher crea un Hasher. cost <= 0 usa DefaultBcryptCost.
func NewHasher(cost int) *Hasher {
	if cost <= 0 {
		cost = DefaultBcryptCost
	}
	return &Hasher{cost: cost}
}

// Hash devuelve el hash bcrypt de la contraseña en texto plano.
func (h *Hasher) Hash(plain []byte) (string, error) {
	b, err := bcrypt.GenerateFromPassword(plain, h.cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Compare verifica contraseña contra hash bcrypt.
func (h *Hasher) Compare(hash string, plain []byte) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), plain)
}
