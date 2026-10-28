// Package cmd implementa la CLI del backend con subcomandos Cobra.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// configFile es la ruta al archivo .env opcional (flag --config).
var configFile string

// rootCmd es el comando raíz del binario junkbox-backend.
var rootCmd = &cobra.Command{
	Use:   "junkbox-backend",
	Short: "Backend de Junkbox (API REST)",
	Long: `Backend en Go de Junkbox: reseñas musicales, calificaciones, red social
y recomendaciones con búsqueda vectorial (pgvector).`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "",
		"archivo .env de configuración (opcional; el entorno tiene prioridad)")
}

// Execute ejecuta la CLI y devuelve el error (si lo hay) sin imprimirlo:
// lo imprime main con formato controlado.
func Execute() error {
	return rootCmd.Execute()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
