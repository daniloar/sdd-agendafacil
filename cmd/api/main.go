// Command api é o entrypoint HTTP (Echo). A História 1 entrega apenas o caminho
// de boot: carregar config, aplicar migrações e parar — a camada HTTP chega numa
// história posterior.
package main

import (
	"context"
	"log"

	"github.com/agendafacil/agendafacil/internal/platform/boot"
)

func main() {
	if err := boot.Run(context.Background(), "api"); err != nil {
		log.Fatal(err)
	}
	log.Print("api: HTTP layer not implemented yet (Story 2+)")
}
