package dto

// RatingRequest es el cuerpo de POST /api/v1/ratings. Score es puntero para
// distinguir un score ausente de uno enviado.
type RatingRequest struct {
	TargetType string   `json:"target_type"`
	TargetID   string   `json:"target_id"`
	Score      *float64 `json:"score"`
}

// CreateReviewRequest es el cuerpo de POST /api/v1/reviews.
type CreateReviewRequest struct {
	TargetType string  `json:"target_type"`
	TargetID   string  `json:"target_id"`
	Title      *string `json:"title,omitempty"`
	Content    string  `json:"content"`
}

// UpdateReviewRequest es el cuerpo de PUT /api/v1/reviews/{id}. Reemplaza
// título y contenido: omitir title deja la reseña sin título.
type UpdateReviewRequest struct {
	Title   *string `json:"title,omitempty"`
	Content string  `json:"content"`
}
