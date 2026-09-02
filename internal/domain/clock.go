package domain

import "time"

// Clock é a porta pela qual toda leitura de tempo do domínio e da aplicação
// acontece (AD-6). Implementações devem retornar UTC. Nenhum código em
// internal/domain ou internal/app pode ler o relógio de parede diretamente —
// toda aritmética de janela (soft lock de 15m, janela de aceite de 30m,
// cancelamento de 2h, lembrete D-1, teto de 24h, ...) é calculada a partir de
// Clock.Now().
type Clock interface {
	// Now retorna o instante atual em UTC (t.Location() == time.UTC).
	Now() time.Time
}
