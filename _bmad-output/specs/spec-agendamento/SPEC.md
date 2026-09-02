---
id: SPEC-agendamento
companions:
  - ../../planning-artifacts/architecture/architecture-sdd-agendafacil-2026-09-01/ARCHITECTURE-SPINE.md
  - ../../planning-artifacts/prds/prd-sdd-agendafacil-2026-09-01/prd.md
sources:
  - ../../planning-artifacts/briefs/brief-sdd-agendafacil-2026-09-01/brief.md
  - ../../planning-artifacts/briefs/brief-sdd-agendafacil-2026-09-01/addendum.md
  - ../../planning-artifacts/architecture/architecture-sdd-agendafacil-2026-09-01/SOLUTION-DESIGN.md
---

> **Contrato canônico.** Este SPEC e os arquivos em `companions:` são o contrato completo, validado por preservação, do que construir, testar e validar. Os documentos-fonte no frontmatter servem apenas para rastreabilidade.
>
> Leia os dois companions junto com este arquivo: `prd.md` traz as consequências testáveis de cada FR, o glossário (§3), a superfície de API (§11) e as métricas de sucesso (§7); `ARCHITECTURE-SPINE.md` traz as decisões de arquitetura vinculantes `AD-1..AD-11`, as máquinas de estado de slot e consulta, e as convenções de consistência. As capacidades abaixo citam `FR-*` (PRD) e `AD-*` (spine); implemente considerando os três em conjunto.

# AgendaFácil — Núcleo de Agendamento (Épico: Agendamento)

## Por quê

Uma dor a resolver, formulada como um mandato de engenharia. Agendar consultas é trivial com um único usuário ativo e quebra no instante em que há dois: dois Pacientes confirmam o mesmo Slot no mesmo segundo e ambos saem com reserva; um Slot cancelado fica ocioso enquanto Pacientes esperam; a prioridade VIP deixa um Paciente comum esperando para sempre; um No-show queima um Slot sem sinal antecipado; uma rede instável transforma um único clique em "confirmar" em duas Consultas ou duas Penalidades. A solução simples do lado da clínica — um lock pessimista de linha mantido enquanto o Paciente decide — serializa o atendimento e não escala. Este épico é o produto inteiro: o motor de agendamento concorrente — publicação de agenda, reserva otimista, expiração confiável, uma Fila de Espera de dois escopos justa e auditável, escritas idempotentes, um ciclo de vida completo de No-show, uma trilha de auditoria imutável, tempo em UTC de ponta a ponta. Afetados: Pacientes (querem um Slot que seja certamente seu e uma fila justa), Médicos (querem que a agenda do dia corresponda à realidade, além de um sinal antecipado de No-show), o Administrador (publica Médicos e Slots) e o ator Sistema (executa expiração, avanço de fila, lembretes e marcação de No-show sem supervisão). Importa agora porque acertar consistência e auditabilidade primeiro é o que faz de cada módulo adiado — pagamento, teleconsulta, prontuário — um incremento em vez de uma reescrita. As metas numéricas existem para ser verificadas em um teste de contenção, não para descrever capacidade de produção.

## Capacidades

- **CAP-1 — Descobrir a agenda publicada** (FR-4)
  - **intenção:** Um Paciente pode listar os Slots de um Médico para um intervalo de datas e ver a disponibilidade de cada Slot para escolher um para reservar.
  - **sucesso:** Apenas Slots `livre` aparecem como disponíveis; `reservado`/`confirmado`/`em_risco`/`bloqueado` aparecem como indisponíveis ou são omitidos quando a requisição passa `only=available`; os horários são UTC com o offset da clínica anexado como metadado de apresentação; um Slot cujo Soft Lock expirou volta a aparecer como disponível sem ação do Paciente (semântica de leitura preguiçosa, AD-2). Somente leitura — sem transação de escrita.

- **CAP-2 — Aplicar Soft Lock em um Slot livre** (FR-5)
  - **intenção:** Um Paciente pode colocar uma reserva de 15 minutos em um Slot `livre` para ter tempo exclusivo de confirmar.
  - **sucesso:** Sob K requisições concorrentes para o mesmo Slot `livre` (K até 50), **exatamente uma** cria a reserva via `UPDATE ... WHERE slot_id=? AND status='livre'` com `rows_affected=1`; as demais recebem `409`; a vencedora define `held_by` e `held_until = now()+15m` (UTC) e escreve `slot:<id>:hold` no Redis `EX 900`; aplicar lock em um Slot não-`livre` → `409`; um Paciente pode manter no máximo **1 Soft Lock ativo por Médico+data** (`409` além disso); nenhum caminho de código adquire lock pessimista de linha. Regido por AD-2, AD-6, AD-7, AD-8, AD-11.

- **CAP-3 — Confirmar um Slot reservado em uma Consulta** (FR-7)
  - **intenção:** O Paciente que mantém a reserva de um Slot pode confirmá-la para criar a Consulta, e repetir a confirmação nunca cria uma segunda.
  - **sucesso:** `reservado→confirmado` via CAS protegido por `held_by=? AND held_until > now()`; expiração no instante da confirmação → `409` + uma Oferta para entrar na Fila de Espera; o índice único parcial em `slot_id WHERE appointment.status='confirmada'` rejeita qualquer segunda Consulta confirmada mesmo que o CAS da aplicação seja contornado; repetir a mesma `Idempotency-Key` retorna a primeira Consulta inalterada; repetir com uma chave *diferente* quando o Paciente já tem uma Consulta naquele Slot retorna a Consulta existente, não um erro. Regido por AD-1, AD-2, AD-5, AD-7, AD-10, AD-11.

- **CAP-4 — Expirar um Soft Lock abandonado** (FR-6)
  - **intenção:** Uma reserva não confirmada dentro da sua janela volta ao pool para que o Slot possa ser reservado de novo.
  - **sucesso:** Qualquer caso de uso de caminho de escrita que leia `status='reservado' AND held_until < now()` executa o CAS `reservado→livre` (via o método único de transição, emitindo o evento de outbox e o registro de auditoria na mesma transação) antes de prosseguir; o worker do Sistema faz o mesmo proativamente a partir do TTL do Redis; um caminho somente leitura que não consegue abrir uma transação de escrita trata o Slot como livre apenas para exibição; o atraso entre a expiração e a liberação efetiva é ≤ 30 s (SM-5). Regido por AD-2, AD-6, AD-7, AD-8, AD-11.

- **CAP-5 — Cancelar uma Consulta confirmada com Penalidade por atraso** (FR-8, FR-9)
  - **intenção:** Um Paciente pode cancelar uma Consulta confirmada; cancelar menos de 2 horas antes do início do Slot registra uma Penalidade de 50% como devida.
  - **sucesso:** o CAS `confirmado→cancelado` tem sucesso uma vez; quando `start - now() < 2h` (UTC, via `Clock`) uma linha `cancellation_penalty` é escrita na mesma transação com `amount` = 50% do Valor de Referência do Slot (fixado na criação do Slot) e sua moeda; o Slot então vai para `livre` e emite `slot.cancelled`, disparando o avanço da Fila de Espera (CAP-7); cancelar com ≥ 2h de antecedência não escreve Penalidade; uma Consulta produz no máximo uma Penalidade sob cancelamentos concorrentes/repetidos; cancelar uma Consulta `concluida`/`no_show` → `409`. Sem cobrança ou coleta. Regido por AD-5, AD-6, AD-7, AD-8, AD-10, AD-11.

- **CAP-6 — Entrar em uma Fila de Espera (por Slot e por Médico+data)** (FR-10, FR-15)
  - **intenção:** Quando nenhum Slot `livre` está disponível, um Paciente pode entrar na Fila de Espera de um Slot específico e/ou de qualquer Slot de um Médico em uma data, para receber a Oferta da próxima vaga.
  - **sucesso:** Uma linha `waitlist_entry` (autoridade) é escrita na transação do caso de uso com `scope` (`slot:<id>` ou `doctor:<id>:<yyyy-mm-dd>` UTC), `enqueued_at`, `class`; um `ZADD` de melhor esforço a espelha para o Redis sorted set correspondente; um Paciente aparece no máximo uma vez por fila (repetição / mesma `Idempotency-Key` retorna a posição existente); entrar na Fila de Espera de um Slot onde o Paciente já tem uma Consulta `confirmada` → `409`. Regido por AD-3, AD-4, AD-5, AD-9.

- **CAP-7 — Avançar a Fila de Espera e fazer uma Oferta de 30 minutos** (FR-12, FR-13)
  - **intenção:** Quando um Slot de um Médico-dia é liberado, o Sistema o oferece — nomeando esse Slot — ao próximo Paciente elegível na Fila de Espera nos dois escopos de fila, com uma janela limitada para aceitar antes de seguir adiante.
  - **sucesso:** Quando um Slot passa a `livre` (cancelamento FR-8, expiração FR-6, No-show FR-22), a próxima entrada pela ordenação da CAP-8 sobre os candidatos **mesclados** de escopo-de-Slot + escopo-de-Médico+data é selecionada; `offer:<slot_id>` é definido com `EX 1800` e uma linha `offers` é inserida via `INSERT ... ON CONFLICT DO NOTHING` no índice único parcial `offers(slot_id) WHERE status='pendente'` de modo que exista no máximo **uma Oferta pendente por Slot** e liberação+avanço concorrentes convirjam; o payload da Oferta nomeia o `slot_id`; o relógio de 30 minutos começa no **envio** da notificação; na expiração ou recusa o Sistema avança para a próxima entrada, ou deixa o Slot `livre` se não restar nenhuma; uma Fila de Espera vazia deixa o Slot `livre`; um Paciente atendido é removido de **todas** as filas daquele Médico+data. Regido por AD-3, AD-4, AD-5, AD-8, AD-9, AD-11.

- **CAP-8 — Prioridade VIP com anti-starvation por quota + teto** (FR-16, FR-17, FR-18)
  - **intenção:** Pacientes VIP são atendidos à frente dos Pacientes comuns na mesma fila, mas a espera de um Paciente comum é limitada independentemente do fluxo de entrada de VIPs.
  - **sucesso:** A ordenação é uma função de domínio pura e determinística: (1) classe — `VIP` antes de `comum`; (2) dentro de uma classe — ordem de entrada (`enqueued_at` UTC); (3) **quota** — um contador de concessões-VIP-consecutivas por Médico+data; passado `ANTI_STARVATION_QUOTA` (padrão 3, configurável) a próxima Oferta vai para o primeiro `comum` elegível, e o contador zera em qualquer concessão a um `comum`; (4) **teto** — um `comum` esperando mais que `ANTI_STARVATION_CEILING` (padrão 24h, configurável) recebe prioridade máxima à frente de qualquer VIP. Mesmo estado de fila + mesmo contador ⇒ mesmo próximo Paciente, nenhum empate resolvido ao acaso; com fluxo contínuo de entrada de VIPs, um `comum` é atendido dentro de `N+1` liberações no pior caso; espera máxima de `comum` ≤ 24h + 10% (SM-4). Regido por AD-4, AD-9.

- **CAP-9 — Enviar o Lembrete D-1 de presença** (FR-19)
  - **intenção:** Um dia antes do Slot, o Sistema envia ao Paciente um lembrete com um pedido de Confirmação de Presença.
  - **sucesso:** Para cada Consulta `confirmada` cujo Slot começa no dia seguinte, o Sistema envia um lembrete via `NotificationGateway` (a implementação de referência o registra em log), com chave determinística `reminder:<appointment_id>:D-1`; reexecutar o job no mesmo dia não reenvia; Consultas `cancelada`/`concluida`/`no_show` não recebem nenhum; o disparo é ~18:00 no fuso da clínica convertido para UTC no agendamento. Regido por AD-5, AD-6, AD-8.

- **CAP-10 — Envelope de idempotência em toda mutação** (FR-25)
  - **intenção:** Qualquer escrita pode ser repetida com segurança por um cliente ou uma automação e produzir exatamente um efeito.
  - **sucesso:** Toda rota mutante (FR-1, 2, 3, 5, 7, 8, 10, 11, 14, 20, 23, 24) exige um cabeçalho `Idempotency-Key` (UUID) — ausente → `400`; a linha da chave é inserida com `ON CONFLICT DO NOTHING` com verificação de linhas afetadas **na mesma transação** que a mutação de domínio, com status/corpo da resposta armazenados; mesma chave + mesmo fingerprint repete a resposta armazenada com zero efeitos colaterais; mesma chave + corpo **diferente** → `422`; duas requisições concorrentes com a mesma chave → uma executa, a outra aguarda e retorna a resposta original ou recebe `409 "em processamento"` (pergunta em aberto); as chaves expiram após 24h (fixo) e uma repetição posterior é tratada como nova, depois limitada pelos invariantes de domínio. Regido por AD-5.

- **CAP-11 — Eventos de domínio via outbox transacional** (parte de FR-26)
  - **intenção:** Toda mudança de estado é registrada de forma durável e entregue a consumidores internos sem acoplar o agendador a eles por chamada direta.
  - **sucesso:** Cada transição escreve um evento no passado (`slot.reserved`, `slot.confirmed`, `slot.cancelled`, `slot.at_risk`, `appointment.no_show`, `offer.made`, `offer.expired`, `offer.accepted`, `penalty.assessed`, …) em `slot_events` na transação da mutação; um relay publica as linhas do outbox em um Redis stream; os consumidores (avanço da Fila de Espera, agendador de lembretes, registrador de Penalidades) são idempotentes e seguros para reprocessamento. Regido por AD-8.

- **CAP-12 — Reconciliação da Fila de Espera a partir das tabelas de autoridade** (AD-9)
  - **intenção:** As filas e o contador de anti-starvation sobrevivem a um reinício ou desvio do Redis porque podem ser reconstruídos inteiramente a partir do Postgres.
  - **sucesso:** Na inicialização do worker e periodicamente, tanto os Redis sorted sets (por Slot e por Médico+data) quanto o `waitlist_vip_counter` (uma linha por Médico+data) são reprojetados a partir de `waitlist_entry` / da tabela do contador (idempotente); "próximo elegível" lê o Redis e, em falha de leitura ou inconsistência detectada, recorre à função de domínio da CAP-8 sobre o Postgres. Regido por AD-9.

- **CAP-13 — Registrar um Médico** (FR-1)
  - **intenção:** Um Administrador pode registrar um Médico para que Slots possam ser publicados para ele.
  - **sucesso:** Um Médico é criado com nome, especialidade e um Valor de Referência padrão; um Médico recém-registrado tem uma Agenda vazia (zero Slots); registrar sem um Valor de Referência → `422`; idempotente (repetição retorna o mesmo Médico, sem duplicata). Regido por AD-1, AD-5, AD-11.

- **CAP-14 — Publicar Slots em lote** (FR-2)
  - **intenção:** Um Administrador pode publicar muitos Slots para um Médico em uma única requisição, inclusive para montar cenários de teste de contenção deliberada.
  - **sucesso:** Todos os Slots criados são `livre` e carregam o Valor de Referência do Médico no momento da publicação; intervalos sobrepostos para o mesmo Médico rejeitam o **lote inteiro** com `422` (nenhum Slot criado); um intervalo com fim ≤ início → `422`; a repetição idempotente retorna o mesmo conjunto de Slots. Regido por AD-1, AD-5, AD-7, AD-11.

- **CAP-15 — Bloquear / desbloquear um Slot livre** (FR-3)
  - **intenção:** Um Administrador ou o Médico dono pode tirar um Slot `livre` de circulação e devolvê-lo.
  - **sucesso:** `livre ↔ bloqueado` tem sucesso; bloquear um Slot que não está `livre` (`reservado`/`confirmado`/`em_risco`) → `409`; um Slot `bloqueado` fica ausente da descoberta (CAP-1) e não pode receber um Soft Lock. Regido por AD-5, AD-7, AD-11.

- **CAP-16 — Aceitar uma Oferta** (FR-14)
  - **intenção:** Um Paciente na Fila de Espera pode aceitar uma Oferta pendente endereçada a ele e obter o Slot.
  - **sucesso:** Aceitar uma Oferta `pendente` endereçada ao Paciente dentro da Janela de Aceite produz uma Consulta `confirmada` no Slot nomeado; idempotente (mesma `Idempotency-Key` → mesma Consulta); aceitar uma Oferta `expirada`/`recusada` ou uma endereçada a outro Paciente → `409`; vale o mesmo invariante de unicidade `(slot, consulta confirmada)` da CAP-3. Regido por AD-5, AD-7, AD-10, AD-11.

- **CAP-17 — Sair de uma Fila de Espera** (FR-11)
  - **intenção:** Um Paciente pode se remover da Fila de Espera de um Slot.
  - **sucesso:** Idempotente — sair de uma fila em que o Paciente não está retorna um resultado neutro; sair enquanto uma Oferta `pendente` está endereçada a ele marca essa Oferta como `recusada` e dispara o avanço (CAP-7). Regido por AD-5, AD-9, AD-11.

- **CAP-18 — Registrar Confirmação de Presença** (FR-20)
  - **intenção:** Um Paciente pode confirmar que vai comparecer, em resposta ao Lembrete D-1.
  - **sucesso:** Idempotente; confirmar presença após o início do Slot → `409` (o caminho válido então é o Check-in, CAP-21). Regido por AD-5, AD-6, AD-7, AD-11.

- **CAP-19 — Marcar um Slot em_risco na ausência de Confirmação de Presença** (FR-21, subconjunto do MVP)
  - **intenção:** A ausência de uma Confirmação de Presença perto do Slot é um sinal antecipado de No-show que sinaliza o Slot sem liberá-lo.
  - **sucesso:** Em `T-2h` (UTC) sem Confirmação de Presença, o Sistema define o Slot como `em_risco`; a Consulta original permanece `confirmada` e o Paciente original mantém a prioridade se comparecer; uma Confirmação de Presença ou Check-in pelo Paciente original devolve o Slot a `confirmado`; o ciclo é idempotente. **Sem pré-oferta no MVP** — uma Oferta real só é criada após um No-show confirmado (CAP-20). Regido por AD-6, AD-7, AD-8, AD-11.

- **CAP-20 — Marcar No-show automaticamente** (FR-22)
  - **intenção:** Um Paciente que não faz Check-in logo após o início do Slot é marcado como ausente para que o Slot possa ser reutilizado.
  - **sucesso:** O Sistema marca como `no_show` qualquer Consulta `confirmada` cujo Slot começou há mais de 15 min sem Check-in; idempotente (reexecução não gera um segundo evento nem uma segunda liberação); um No-show confirmado libera o Slot e dispara a criação da Oferta (CAP-7); toda marcação escreve um registro de auditoria com instante e causa. Regido por AD-5, AD-6, AD-7, AD-10, AD-11.

- **CAP-21 — Registrar Check-in** (FR-23)
  - **intenção:** Um Paciente (ou o Médico em seu nome) pode registrar que o Paciente chegou.
  - **sucesso:** O Check-in é aceito de 30 min antes até 15 min depois do início do Slot; dentro da janela ele impede o No-show automático; idempotente; fora da janela → `409`. Regido por AD-5, AD-6, AD-7, AD-11.

- **CAP-22 — Sobreposição de No-show pelo Médico** (FR-24)
  - **intenção:** O Médico dono pode desfazer um No-show marcado erroneamente ou marcar um manualmente antes do prazo automático.
  - **sucesso:** Desfazer um No-show devolve a Consulta a `confirmada`; desfazer quando o Slot já foi reofertado/reconfirmado para um terceiro → `409` com o conflito declarado (não descarta silenciosamente a Consulta do terceiro); toda sobreposição escreve um registro de auditoria com `doctor_id`; idempotente. Regido por AD-5, AD-7, AD-10, AD-11.

- **CAP-23 — Trilha de auditoria imutável das transições de estado** (FR-27, AD-11)
  - **intenção:** Toda mudança de estado relevante deixa um rastro consultável e à prova de adulteração para que o histórico possa ser reconstruído e os invariantes verificados.
  - **sucesso:** Toda transição de Slot, Consulta, Soft Lock, Oferta e Penalidade escreve um `audit_record` somente-acréscimo (`entity`, `entity_id`, `prev_state`, `new_state`, `actor ∈ {patient_id|doctor_id|system}`, `cause`, `ts` UTC) na **mesma transação** que a mutação; os registros nunca são atualizados nem excluídos; o histórico completo de uma dada entidade é reconstruível a partir da trilha; a contagem de transições de estado é igual à contagem de registros de auditoria (SM-8). Regido por AD-11, AD-8.

- **CAP-24 — Endpoint de métricas de verificação de invariantes** (FR-28, AD-11)
  - **intenção:** O sistema pode afirmar seus invariantes centrais a qualquer momento sem inspeção manual.
  - **sucesso:** Um endpoint expõe contadores/consultas para: zero Slots com duas Consultas `confirmada`, zero Ofertas `pendente` duplicadas por Slot, zero Penalidades duplicadas por Consulta, e a distribuição de tempo de espera na Fila de Espera do Paciente comum; após uma execução de contenção (50 requisições concorrentes por Slot sobre ~1000 Slots) todo contador de violação permanece zero; a métrica de espera máxima do Paciente comum é consultável e está dentro do SM-4. Regido por AD-11.

- **CAP-25 — Jobs do Sistema executam de forma idempotente e observável** (FR-26)
  - **intenção:** Toda automação sem supervisão é segura para reexecução e reporta o que fez.
  - **sucesso:** Os jobs do Sistema (expirar lock, avançar fila, promoção por quota/teto, Lembrete D-1, marcação de em_risco, marcação de No-show) executam em um intervalo configurável, são seguros para reexecução (executar duas vezes sobre o mesmo estado converge — nenhum Slot com lock órfão, nenhuma Oferta pendente sem dono, nenhuma Penalidade parcial), e cada execução emite itens processados / transições feitas / duração. Regido por AD-5, AD-8, AD-11.

## Restrições

- **Sem locks pessimistas de banco no caminho de decisão.** Sem `SELECT … FOR UPDATE`, sem advisory lock bloqueante durante a decisão de um Paciente. Toda mutação de estado é CAS (`UPDATE … WHERE status = <expected>` ou `INSERT … ON CONFLICT`) com verificação de linhas afetadas; zero linhas afetadas = perdeu a corrida = erro de domínio.
- **PostgreSQL é o armazenamento transacional canônico; Redis é apenas um acelerador.** "No máximo uma Consulta `confirmada` por Slot" é um índice único parcial do Postgres em `slot_id WHERE appointment.status = 'confirmada'`, não uma verificação de aplicação. Em caso de divergência, o Postgres vence — `held_until` para o lock, `waitlist_entry` / `waitlist_vip_counter` para a fila.
- **O estado do Slot é uma máquina explícita com um único escritor.** Estados `livre | reservado | confirmado | em_risco | bloqueado`; todas as transições passam por um único método de domínio `Slot.transition(to, guard)` que emite o CAS. Apenas o caso de uso de agendamento escreve `slots.status` — nenhum caminho de leitura, worker de fila, job de No-show, job de lembrete ou consumidor de evento o escreve diretamente.
- **O estado da Consulta é sua própria máquina explícita com um único escritor.** Estados `confirmada | cancelada | concluida | no_show`; as transições passam por `Appointment.transition(to, guard)` no mesmo caso de uso e na mesma transação que a transição de Slot pareada e o evento de outbox. O job de No-show, o Check-in e a sobreposição do Médico chamam o caso de uso de agendamento — nunca escrevem `appointments.status` diretamente.
- **Toda transição de estado emite, na mesma transação que a mutação, tanto um evento de outbox quanto um registro de auditoria imutável.** Os consumidores são idempotentes e seguros para replay. Registros de auditoria nunca são atualizados nem excluídos; a contagem de transições deve ser igual à contagem de registros de auditoria.
- **Toda mutação de API e toda automação do Sistema é idempotente.** `Idempotency-Key` (UUID) obrigatória nas rotas mutantes; ausente → `400`; mesma chave + corpo diferente → `422`; chave e resposta persistidas na mesma transação que a mutação. As automações do Sistema derivam uma chave determinística de `(type, entity, window)`.
- **Todo tempo é UTC internamente.** Todo timestamp persistido é `timestamptz` UTC. Toda janela (15 min, 30 min, 2 h, D-1, T-2h, +15 min de fim do Check-in, −30 min de início do Check-in, teto de 24 h) é computada no domínio via uma porta `Clock` injetável, nunca a partir do relógio de parede da requisição. Conversão de fuso apenas no serializador da borda HTTP. Os invariantes de unicidade não dependem da sincronia de relógio entre aplicação e banco.
- **Dois escopos de Fila de Espera, uma seleção mesclada.** `waitlist:slot:<slot_id>` e `waitlist:doctor:<doctor_id>:<yyyy-mm-dd>` (UTC). Um Slot liberado mescla candidatos de ambos, ordenados pela regra de ordenação de fila abaixo. No máximo uma Oferta `pendente` por Slot, garantida por `INSERT ON CONFLICT` em um índice único parcial. Um Paciente atendido sai de todas as filas daquele Médico+data.
- **A ordenação de fila é uma função de domínio pura e determinística:** classe (`VIP` antes de `comum`) e depois ordem de entrada; anti-starvation = quota (`ANTI_STARVATION_QUOTA`, padrão 3 — contador de concessões-VIP-consecutivas por Médico+data, zera em qualquer concessão a um `comum`) + teto (`ANTI_STARVATION_CEILING`, padrão 24h — um `comum` que o ultrapassa recebe prioridade máxima). Sem fila VIP separada, sem score composto. O Redis guarda apenas o resultado computado.
- **A Penalidade é registrada como devida, nunca cobrada.** `cancellation_penalty` armazena `amount` = 50% do Valor de Referência do Slot (herdado do Médico na publicação, fixado na criação do Slot) e `currency`; não existe caminho de pagamento, faturamento, coleta, reembolso ou disputa neste épico.
- **A identidade é um claim confiável de middleware** (`patient_id` / `doctor_id`); o Sistema é um ator de job interno. Autenticação, sessão e registro de identidade estão fora de escopo.
- **Fronteiras hexagonais.** O domínio declara portas; os adaptadores (`http`, `postgres`, `redis`, `worker`) as implementam; o domínio não importa nada de `adapter`/`platform`. As notificações passam pelo `NotificationGateway` (implementação de referência = log/fake). O worker do ator Sistema invoca os mesmos casos de uso de aplicação que a API HTTP.
- **Política de versão da API:** mudanças incompatíveis apenas em uma nova major de rota (`/v2/...`); campos novos são aditivos.

## Não-objetivos

- Pagamento, cobrança, faturamento, coleta, reembolso ou disputa da Penalidade de cancelamento.
- Teleconsulta, prontuário eletrônico, prescrição, ou armazenamento de qualquer dado clínico.
- Frontend rico, app nativo ou UI web — o entregável é a API.
- Autenticação, registro de identidade, gerenciamento de sessão, ou autorização multi-papel além de distinguir atores por um claim confiável.
- Canais reais de notificação (email/SMS/push) — apenas `NotificationGateway` com um adaptador de log de referência.
- Gestão avançada de disponibilidade: recorrência, feriados, bloqueio/realocação em massa de Slots já confirmados, realocação automática de Pacientes.
- Pré-oferta condicional em Slots `em_risco` (FR-21 na forma completa) — adiada para a v2; o MVP marca `em_risco` e oferta apenas após um No-show confirmado.
- Isenção de Penalidade por regra clínica (emergência, atestado médico) — v2.
- Renovação de Soft Lock (um Paciente pedindo mais tempo) — não na v1.
- Persistência da resposta idempotente além de 24h.
- Multi-tenant / múltiplas unidades de clínica / descoberta pública de Médicos.
- Um message broker (RabbitMQ/Kafka) entre serviços.
- Capacidade/escala de produção; SLA de produção; meta de custo de infraestrutura; RTO/RPO; deploy além do Docker Compose (Kubernetes, autoscaling).

## Sinal de sucesso

Sob o teste de contenção deliberada na meta de escala do PRD (~50 Médicos, ~1000 Slots/dia, 200 Pacientes concorrentes, até 50 requisições concorrentes por Slot):

- **SM-1** zero Slots com duas Consultas `confirmada`;
- **SM-2** 100% das repetições (mesma `Idempotency-Key`) retornam a resposta original sem novo efeito colateral;
- **SM-3** ≥ 80% dos Slots liberados com Fila de Espera não vazia se tornam uma Consulta `confirmada` para um Paciente na fila dentro de uma Janela de Aceite;
- **SM-4** espera máxima na Fila de Espera do Paciente comum ≤ 24h + 10% sob fluxo contínuo de entrada de VIPs;
- **SM-5** p95 do atraso entre um gatilho (lock expirado, Janela de Aceite expirada, No-show) e a transição de estado efetiva ≤ 30 s;
- **SM-6** zero erros de limite de janela (15 min / 30 min / 2 h / D-1 / 24 h) em uma suíte com clientes em fusos variados;
- **SM-8** a contagem de registros de auditoria é igual à contagem de transições de estado.

Um Médico que abre a agenda do dia vê um Paciente real por linha e nenhuma reserva dupla; o endpoint de métricas de invariantes reporta todos os contadores de violação em zero.

## Premissas

- O segundo corte D-1 (Slot marcado `em_risco`) é `T-2h` antes do início do Slot; pré-oferta condicional adiada para a v2.
- A Janela de Aceite de 30 minutos conta a partir do **envio** da notificação, não da entrega.
- O TTL da `Idempotency-Key` é 24h, fixo; uma repetição após a expiração é tratada como nova.
- O Lembrete D-1 é disparado ~18:00 no fuso da clínica, convertido para UTC no agendamento do job.
- Um Paciente mantém no máximo 1 Soft Lock ativo por Médico+data.
- O No-show é marcado automaticamente 15 min após o início do Slot sem Check-in; a janela de Check-in é de −30 min a +15 min em torno do início do Slot.
- Desfazer um No-show depois de o Slot ter sido reconfirmado para um terceiro retorna `409`.
- Base da Penalidade = 50% do Valor de Referência do Slot fixado na criação do Slot, não relido depois.
- Os IDs são UUIDv7; o discriminador de concorrência é `status` + timestamps, não uma coluna numérica `version`.
- O Redis roda com `appendonly yes` (AOF); a perda tolerável no reinício é o último segundo.
- O Soft Lock não é renovável na v1.
- Repetição com a mesma chave + corpo diferente → `422`; mesma chave concorrente → uma executa, a outra aguarda ou recebe `409 "em processamento"`.
- Mudanças incompatíveis de API apenas em uma nova major de rota.

## Perguntas em aberto

- **Idempotência concorrente com a mesma chave (FR-25):** a segunda requisição bloqueia até a resposta, ou retorna `409 "em processamento"`?
- **Origem da classe VIP (FR-16):** o claim de autenticação confiável carrega a classe VIP do Paciente, ou ela é consultada internamente?
- **Calibração do anti-starvation (PRD Q3):** `ANTI_STARVATION_QUOTA=3` e `ANTI_STARVATION_CEILING=24h` são adequados para o volume esperado? Deveriam variar por especialidade?
- **Disparo D-1 (PRD Q7):** o disparo às 18:00 no fuso da clínica é configurável por clínica, e o que fazer com uma clínica com Slots em múltiplos fusos?
- **Conflito de desfazer No-show (FR-24 / PRD Q6):** `409` é a resposta final após a reconfirmação por um terceiro, ou existe um fluxo de resolução?
- **Política de retry/backoff do relay do outbox** — deixada para o spec pela spine (Adiada); ainda não especificada.
- **Escopo de proteção de dados (PRD §12):** armazenar um contato de notificação por Paciente coloca o sistema no escopo de dados sensíveis (LGPD)? O PRD sinaliza isso como não confirmado; a spine adia a conformidade para a arquitetura.
