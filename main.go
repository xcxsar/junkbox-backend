// junkbox-backend es el binario del backend de Junkbox.
// Ver cmd/ para los subcomandos (server).
package main

import (
	"fmt"
	"os"
	// Base de zonas horarias embebida: el feed "Para ti" valida ?tz= y la
	// imagen Docker no depende de tener tzdata instalado.
	_ "time/tzdata"

	"github.com/xcxsar/junkbox-backend/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
