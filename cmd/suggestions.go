package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/xcxsar/junkbox-backend/config"
	"github.com/xcxsar/junkbox-backend/internal/adapters/secondary/postgres"
	"github.com/xcxsar/junkbox-backend/internal/core/services"
	"github.com/xcxsar/junkbox-backend/internal/store/sqlc"
)

var suggestionsCmd = &cobra.Command{
	Use:   "suggestions",
	Short: "Sugerencias de usuarios (HOME-05)",
}

var suggestionsRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Recalcula los perfiles acústicos y las sugerencias de todos los usuarios",
	Long: `Recalcula ahora lo mismo que el worker de sugerencias del servidor
(que lo hace al arrancar y cada SUGGESTIONS_REFRESH_INTERVAL).`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.Load(configFile)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		db, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.Options{})
		if err != nil {
			return fmt.Errorf("db: %w", err)
		}
		defer db.Close()

		svc := services.NewSuggestionService(sqlc.New(db), postgres.NewSuggestionRefresher(db))
		start := time.Now()
		profiles, suggestions, err := svc.Refresh(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%d perfiles acústicos y %d sugerencias en %s\n", profiles, suggestions, time.Since(start).Round(time.Millisecond))
		return nil
	},
}

func init() {
	suggestionsCmd.AddCommand(suggestionsRefreshCmd)
	rootCmd.AddCommand(suggestionsCmd)
}
