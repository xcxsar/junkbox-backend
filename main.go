// junkbox-backend es el binario del backend de Junkbox.
// Ver cmd/ para los subcomandos (server).
package main

import (
	"fmt"
	"os"

	"github.com/xcxsar/junkbox-backend/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
