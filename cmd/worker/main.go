// Command worker é o entrypoint do ator Sistema (jobs agendados + outbox relay).
// A História 1 entrega apenas o caminho de boot: carregar config, aplicar
// migrações e parar — os jobs e o relay chegam numa história posterior.
package main

import (
	"context"
	"log"

	"github.com/agendafacil/agendafacil/internal/platform/boot"
)

func main() {
	if err := boot.Run(context.Background(), "worker"); err != nil {
		log.Fatal(err)
	}
	log.Print("worker: System jobs and outbox relay not implemented yet (Story 2+)")
}
