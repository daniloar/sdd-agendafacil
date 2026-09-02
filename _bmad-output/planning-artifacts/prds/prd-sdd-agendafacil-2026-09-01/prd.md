---
title: AgendaFácil
status: final
created: 2026-09-01
updated: 2026-09-01
---

# PRD: AgendaFácil
*Título de trabalho — confirmar.*

## 0. Propósito do Documento

Este PRD é para o PM, os revisores de arquitetura/solution design e os workflows
downstream (UX mínima de API, arquitetura, épicos e histórias). Ele descreve
**capacidades**, não implementação: o motor de agendamento de consultas médicas
do AgendaFácil como um **backend API-only**, cujo valor está na correção sob
concorrência. Está estruturado com vocabulário ancorado no Glossário (§3),
features agrupadas com Requisitos Funcionais (FR) aninhados e numerados
globalmente, premissas marcadas inline com `[ASSUMPTION]` e indexadas em §9.
Constrói sobre o **Product Brief** e o **Addendum** de 2026-09-01
(`_bmad-output/planning-artifacts/briefs/brief-sdd-agendafacil-2026-09-01/`) —
não os duplica. Escolhas de stack e mecanismo de transação (Go/Echo, PostgreSQL,
Redis, estratégias de reserva otimista) vivem no Addendum e na arquitetura, não
aqui.

Contexto de uso: artefato de referência para desenvolvimento orientado a
especificação (SDD). Os alvos numéricos existem para serem **verificáveis em
teste**, não para descrever capacidade de produção.

## 1. Visão

O AgendaFácil é o núcleo de agendamento de um sistema de consultas médicas. Um
paciente escolhe médico, data e horário; o sistema segura esse horário por uma
janela curta; o paciente confirma; a consulta fica marcada. Quando não há vaga,
o paciente entra numa fila de espera que avança sozinha e de forma justa à
medida que horários são liberados.

O que torna o AgendaFácil diferente não é a tela de calendário — é o que
acontece **quando há mais de um usuário ativo ao mesmo tempo**. Dois pacientes
clicam no mesmo horário no mesmo segundo e apenas um sai com a consulta. Um
cancelamento em cima da hora é reaproveitado por quem está esperando, dentro de
uma janela previsível. Uma fila com pacientes VIP nunca deixa um paciente comum
para trás indefinidamente. E qualquer operação de escrita pode ser repetida — por
retry de rede, duplo clique ou reenvio automático — sem criar uma segunda
consulta nem uma segunda penalidade.

O AgendaFácil trata agendamento como um problema de consistência distribuída,
resolvido **sem trava pessimista no banco**: reserva otimista, expiração
confiável, fila auditável e todas as operações idempotentes, com tempo mantido
em UTC internamente. Se esse núcleo estiver sólido, módulos de maior risco —
cobrança da penalidade, teleconsulta, integração com prontuário, políticas de
fila por especialidade — podem ser acrescentados de forma incremental em vez de
exigir reescrita.

## 2. Usuário-Alvo

### 2.1 Jobs To Be Done

- **Paciente — funcional:** marcar uma consulta rapidamente e ter certeza de que
  o horário é dele do início ao fim do fluxo de confirmação.
- **Paciente — emocional:** não sentir medo de "perder o horário no meio do
  caminho" nem de estar numa fila onde nunca chega a vez.
- **Paciente — funcional:** quando não há vaga, entrar numa fila e ser avisado a
  tempo quando uma abre, com uma janela clara para aceitar.
- **Médico — funcional:** ter a agenda do dia fiel à realidade — sem
  double-booking, sem paciente fantasma — e um sinal antecipado de quem
  provavelmente não vem.
- **Sistema (automação) — funcional:** expirar reservas, avançar filas e
  disparar lembretes de forma idempotente e observável, sem intervenção humana.
- **Time de engenharia (builder) — contextual:** exercitar concorrência,
  idempotência e regras de negócio bem definidas sobre uma base de referência
  correta e auditável.

### 2.2 Não-Usuários (v1)

- **Marketplace aberto de médicos / pacientes de fora de uma clínica.** O
  contexto é uma clínica ou rede pequena com agenda publicada por um
  administrador. `[ASSUMPTION]`
- **Operador de cobrança / financeiro.** A Penalidade é registrada como devida;
  ninguém a cobra dentro deste sistema na v1.
- **Recepcionista fazendo encaixe manual na tela.** A automação do Sistema, não
  um humano, avança filas e reaproveita vagas na v1.
- **Paciente sem autenticação prévia.** A identidade do Paciente chega ao motor
  já autenticada (§10). `[ASSUMPTION]`

### 2.3 Principais Jornadas de Usuário

*Narrativas com persona nomeada que o produto habilita. Numeradas UJ-1..UJ-N.
Os FRs referenciam jornadas por ID inline ("realiza UJ-3"). Como não há frontend
no escopo, os "passos" são chamadas de API vistas do ponto de vista da pessoa que
opera um cliente fino.*

- **UJ-1. Rita marca uma consulta e confirma sem medo de perder o horário.**
  - **Persona + contexto:** Rita, 34, precisa de um clínico geral e tem 5 minutos
    entre reuniões. Quer resolver agora.
  - **Estado inicial:** autenticada; consultando a Agenda publicada de um Médico.
  - **Caminho:** (1) lista Slots livres do Dr. Alves para quinta; (2) seleciona o
    de 14h — o sistema cria um **Soft Lock** de 15 min em nome dela e responde com
    o prazo de expiração; (3) Rita confirma dentro da janela, enviando uma
    **Chave de Idempotência**; (4) o cliente dela perde a resposta por rede e
    reenvia a mesma requisição.
  - **Clímax:** a segunda requisição retorna **exatamente a mesma Consulta
    confirmada** — sem segunda consulta, sem erro. Rita vê "confirmado, 14h,
    Dr. Alves".
  - **Resolução:** Consulta no estado *confirmada*; Rita recebe um Lembrete D-1
    na véspera.
  - **Edge case:** se o Soft Lock dela tiver expirado e outro Paciente já tiver
    pego o Slot, a Confirmação retorna 409 e oferece entrada na Fila de Espera.

- **UJ-2. Bruno perde o Slot na disputa e é reaproveitado pela fila minutos
  depois.**
  - **Persona + contexto:** Bruno tenta o mesmo horário concorrido que outra
    pessoa.
  - **Estado inicial:** autenticado; dois Pacientes confirmando o mesmo Slot
    quase simultaneamente.
  - **Caminho:** (1) Bruno confirma; a transação atômica aceita o outro Paciente;
    (2) Bruno recebe 409 e entra na **Fila de Espera** daquele Slot; (3) 30
    minutos depois o outro Paciente cancela; (4) o Sistema cria uma **Oferta**
    para Bruno e registra o instante da notificação.
  - **Clímax:** Bruno aceita dentro da **Janela de Aceite** de 30 min e sai com a
    Consulta confirmada.
  - **Resolução:** vaga reaproveitada sem intervenção humana; se Bruno não
    tivesse aceitado, a Oferta passaria ao próximo elegível.

- **UJ-3. Dr. Alves chega e a agenda do dia está correta.**
  - **Persona + contexto:** Dr. Alves, antes do primeiro atendimento, abre a
    agenda do dia.
  - **Estado inicial:** autenticado como Médico dono da Agenda.
  - **Caminho:** (1) lista as Consultas confirmadas de hoje; (2) vê marcações de
    **em risco** para Pacientes que não confirmaram presença no Lembrete D-1; (3)
    às 8h15 um Paciente não fez Check-in e o Sistema marca **No-show**; (4) Dr.
    Alves sobrepõe o No-show porque o Paciente acabou de chegar.
  - **Clímax:** nenhum horário aparece com duas Consultas; cada linha da agenda
    corresponde a um Paciente real.
  - **Resolução:** agenda confiável; sinais de No-show alimentam o
    reaproveitamento de vaga.

- **UJ-4. Dona Ivone (VIP) tem prioridade, mas Seu Pedro (comum) não espera para
  sempre.** *(forma leve)*
  Dona Ivone, Paciente VIP, entra na Fila de Espera de um Slot concorrido e é
  colocada à frente de Pacientes comuns; a política **Anti-starvation** garante
  que, após um número fixo de concessões consecutivas a VIPs na mesma fila, ou
  quando Seu Pedro ultrapassa o teto de espera, Seu Pedro é promovido à frente da
  próxima Oferta.

## 3. Glossário

*Workflows downstream e leitores devem usar estes termos exatamente. FRs, UJs e
SMs usam os termos do Glossário verbatim; introduzir sinônimo em qualquer ponto
do PRD é violação de disciplina.*

- **Paciente** — Pessoa autenticada que agenda, confirma, cancela Consultas e
  entra na Fila de Espera. Tem exatamente uma de duas classes: **comum** ou
  **VIP**.
- **Paciente VIP** — Paciente com classe VIP; recebe prioridade na Fila de
  Espera, limitada pela política Anti-starvation.
- **Médico** — Ator autenticado dono de uma Agenda; consome a lista de Consultas
  confirmadas e pode sobrepor marcações de No-show. Um Médico tem uma Agenda.
- **Sistema** — Ator interno não-humano: jobs agendados que expiram Soft Locks,
  avançam a Fila de Espera, disparam Lembretes D-1 e marcam No-show.
- **Administrador** — Ator autenticado que cadastra Médicos e publica Slots. Fora
  do fluxo de agendamento em si.
- **Agenda** — Conjunto de Slots de um Médico ao longo do tempo. Uma Agenda
  pertence a um Médico.
- **Slot** — Unidade agendável: um Médico, um intervalo de início/fim em UTC, e
  um estado. Estados: *livre*, *reservado* (há Soft Lock ativo), *confirmado* (há
  Consulta *confirmada*), *em risco* (Consulta sem Confirmação de Presença dentro
  da janela), *bloqueado* (indisponibilizado pelo Administrador/Médico). Um Slot
  carrega um **Valor de Referência**. No máximo uma Consulta *confirmada* por
  Slot.
- **Valor de Referência** — Valor monetário da Consulta associado ao Slot,
  herdado do Médico na publicação. Base de cálculo da Penalidade. Não é cobrado
  pelo sistema.
- **Vaga** — Uso coloquial, equivalente a um Slot no estado *livre* ou a um Slot
  recém-liberado. Não é uma entidade distinta; onde a precisão importa, o texto
  usa "Slot" e seu estado.
- **Soft Lock** — Reserva temporária de um Slot para um Paciente, com prazo de
  expiração fixo de **15 minutos** a partir da criação. Não renovável. Enquanto
  ativo, o Slot aparece como *reservado* para os demais Pacientes.
- **Consulta** — Compromisso confirmado entre um Paciente e um Médico num Slot.
  Estados: *confirmada*, *cancelada*, *concluída*, *no-show*. Uma Consulta
  referencia exatamente um Slot.
- **Confirmação** — Ação idempotente do Paciente que transforma um Soft Lock
  ativo em Consulta *confirmada*.
- **Cancelamento** — Ação idempotente do Paciente que leva uma Consulta
  *confirmada* a *cancelada* e libera o Slot. Pode gerar Penalidade.
- **Penalidade** — Registro de valor devido por um Cancelamento com menos de
  **2 horas** de antecedência do início do Slot: **50%** do Valor de Referência.
  Estado: *devida*. Nunca é cobrada dentro deste sistema.
- **Fila de Espera** — Sequência ordenada de Pacientes aguardando um Slot
  específico. Ordenação por prioridade (VIP antes de comum, sujeita a
  Anti-starvation) e, dentro da mesma prioridade, por ordem de entrada.
- **Oferta** — Proposta de um Slot liberado ao primeiro Paciente elegível da Fila
  de Espera, válida por uma Janela de Aceite. Estados: *pendente*, *aceita*,
  *expirada*, *recusada*.
- **Janela de Aceite** — Prazo de **30 minutos** para aceitar uma Oferta, contado
  a partir do instante de **notificação enviada** registrado pelo Sistema.
- **Anti-starvation** — Política que limita a prioridade VIP na Fila de Espera:
  após **N** concessões consecutivas a Pacientes VIP na mesma Fila de Espera, o
  próximo Paciente comum elegível é promovido à frente; adicionalmente, um
  Paciente comum aguardando há mais que o **teto de espera** ganha prioridade
  máxima. `[ASSUMPTION]` N = 3; teto de espera = 24 h; ambos configuráveis.
- **Lembrete D-1** — Notificação enviada ao Paciente no dia anterior ao Slot,
  contendo um pedido de Confirmação de Presença. `[ASSUMPTION]` Disparado às
  18:00 no fuso da clínica.
- **Confirmação de Presença** — Resposta do Paciente ao Lembrete D-1 indicando
  que comparecerá. Ausência dela até 2 h antes do Slot marca o Slot como *em
  risco*.
- **Check-in** — Registro de que o Paciente compareceu, feito de 30 min antes até
  15 min depois do início do Slot. Ausência de Check-in nesse prazo aciona a
  marcação automática de No-show.
- **No-show** — Estado da Consulta quando o Paciente não fez Check-in no prazo e
  o Médico não sobrepôs.
- **Chave de Idempotência** — Identificador único (UUID) enviado pelo cliente no
  header `Idempotency-Key` em toda operação de escrita mutante. Retida por 24 h;
  requisições repetidas com a mesma chave retornam a resposta original sem novo
  efeito colateral.
- **UTC** — Todo timestamp persistido e toda aritmética de janela (15 min, 30
  min, 2 h, D-1, 24 h) são calculados em UTC. Conversão para fuso local só na
  borda de apresentação.

## 4. Features

*Cada subseção é uma feature coerente: descrição comportamental primeiro, FRs
aninhados, NFRs e notas específicas quando aplicável. FRs numerados globalmente
(FR-1..FR-N).*

### 4.1 Publicação de Agenda

**Descrição:** Um Administrador cadastra Médicos e publica os Slots de cada um.
A publicação em lote é a forma primária de popular uma Agenda — inclusive para
gerar cenários de contenção deliberada em teste. Cada Slot publicado herda o
Valor de Referência do Médico. O Administrador (ou o Médico) pode bloquear e
desbloquear Slots ainda *livres*. Recorrência avançada, feriados e regras
complexas de disponibilidade estão fora de escopo (§5). Realiza UJ-3.

**Requisitos Funcionais:**

#### FR-1: Cadastrar Médico

O Administrador pode cadastrar um Médico com nome, especialidade e Valor de
Referência padrão.

**Consequências (testáveis):**
- Um Médico recém-cadastrado tem uma Agenda vazia (zero Slots).
- Tentar cadastrar Médico sem Valor de Referência retorna 422.
- A operação aceita `Idempotency-Key`; repetição retorna o mesmo Médico sem
  criar duplicata.

#### FR-2: Publicar Slots em lote

O Administrador pode publicar N Slots para um Médico numa única requisição,
informando início e fim de cada Slot em UTC.

**Consequências (testáveis):**
- Todos os Slots criados nascem no estado *livre* e com o Valor de Referência do
  Médico no instante da publicação.
- Slots com sobreposição de intervalo para o mesmo Médico são rejeitados em
  bloco com 422 e nenhum Slot do lote é criado.
- Intervalos com fim ≤ início são rejeitados com 422.
- A operação aceita `Idempotency-Key`; replay retorna o mesmo conjunto de Slots.

#### FR-3: Bloquear e desbloquear Slot livre

O Administrador ou o Médico dono da Agenda pode marcar um Slot *livre* como
*bloqueado* e reverter.

**Consequências (testáveis):**
- Bloquear um Slot que não está *livre* (reservado, confirmado, em risco)
  retorna 409.
- Um Slot *bloqueado* não aparece na descoberta (FR-4) nem pode receber Soft
  Lock.

**Notes:** `[NOTE FOR PM]` Bloqueio de Slot já *confirmado* (ex.: médico ficou
doente) exigiria realocação em massa — deferido; ver §8.

### 4.2 Descoberta de Slots

**Descrição:** Um Paciente autenticado consulta a Agenda publicada de um Médico e
vê quais Slots pode tentar reservar. Slots *reservados* por outro Paciente
aparecem como indisponíveis durante a janela do Soft Lock. Realiza UJ-1, UJ-2.

**Requisitos Funcionais:**

#### FR-4: Listar Slots disponíveis de um Médico

O Paciente pode listar os Slots de um Médico num intervalo de datas, com o estado
de disponibilidade de cada um. Realiza UJ-1.

**Consequências (testáveis):**
- Apenas Slots *livre* aparecem como disponíveis; *reservado*, *confirmado*, *em
  risco* e *bloqueado* aparecem como indisponíveis por padrão, ou são omitidos
  quando a requisição passa `only=available`.
- Os horários retornados estão em UTC, com o offset do fuso da clínica anexado
  como metadado de apresentação.
- Um Slot cujo Soft Lock expirou volta a aparecer como disponível sem ação do
  Paciente (a expiração é resolvida pelo Sistema — FR-6 — ou de forma preguiçosa
  na leitura).

### 4.3 Soft Lock (Reserva Temporária)

**Descrição:** Ao escolher um Slot *livre*, o Paciente cria um Soft Lock que
segura o Slot por 15 minutos. Só um Soft Lock ativo por Slot. O lock **não é
renovável**: se a Confirmação não vier na janela, ele expira e o Slot volta a
*livre*. A resolução de concorrência aqui é o coração do produto — dois Pacientes
tentando travar o mesmo Slot no mesmo instante têm exatamente um vencedor, **sem
trava pessimista no banco**. Realiza UJ-1, UJ-2.

**Requisitos Funcionais:**

#### FR-5: Criar Soft Lock sobre um Slot livre

O Paciente pode criar um Soft Lock sobre um Slot *livre*; o Slot passa a
*reservado* e a resposta traz o instante de expiração (criação + 15 min, em UTC).
Realiza UJ-1.

**Consequências (testáveis):**
- Sob K requisições concorrentes para o mesmo Slot *livre* (K até 50), **exatamente
  uma** cria o Soft Lock; as demais recebem 409.
- Criar Soft Lock sobre Slot *reservado*, *confirmado*, *em risco* ou *bloqueado*
  retorna 409.
- Nenhum caminho de código adquire trava pessimista de linha (`SELECT ... FOR
  UPDATE`) sobre o Slot para tomar essa decisão — verificável por revisão e por
  ausência de serialização observável sob carga.
- Um mesmo Paciente com Soft Lock ativo em um Slot que repete a criação com a
  mesma `Idempotency-Key` recebe o mesmo Soft Lock (sem 409).
- Um Paciente pode ter no máximo `[ASSUMPTION]` 1 Soft Lock ativo por Médico+data
  simultaneamente; exceder retorna 409.

**Fora de Escopo:**
- Renovação/extensão de um Soft Lock ativo.
- Fila de prioridade para "quem trava a seguir" — se não travou, o Paciente usa a
  Fila de Espera (§4.6).

#### FR-6: Expirar Soft Lock vencido

O Sistema expira Soft Locks cujo prazo passou, devolvendo o Slot a *livre* e
acionando o avanço da Fila de Espera daquele Slot (FR-13).

**Consequências (testáveis):**
- Um Soft Lock com expiração no passado nunca impede outro Paciente de travar o
  Slot: ou o Sistema já o expirou, ou a tentativa de travar/confirmar o trata
  como inexistente.
- A expiração é idempotente: reprocessar o mesmo Soft Lock vencido não gera
  segundo evento de avanço de fila.
- O atraso entre o vencimento e a liberação efetiva do Slot é ≤ `[ASSUMPTION]`
  30 s.

### 4.4 Confirmação de Consulta

**Descrição:** Com um Soft Lock ativo, o Paciente confirma e o Slot passa a
*confirmado*, criando uma Consulta. A Confirmação é **idempotente**: reenvios —
por retry de rede, duplo clique ou reenvio automático do cliente — retornam a
mesma Consulta, sem criar uma segunda nem gerar segundo efeito colateral. O caso
crítico é a corrida entre a expiração do Soft Lock e o envio da Confirmação.
Realiza UJ-1, UJ-2.

**Requisitos Funcionais:**

#### FR-7: Confirmar Consulta a partir de um Soft Lock ativo

O Paciente pode confirmar enquanto seu Soft Lock está ativo; o resultado é uma
Consulta *confirmada* no Slot. Realiza UJ-1.

**Consequências (testáveis):**
- Nenhum Slot fica com duas Consultas *confirmadas*, mesmo sob Confirmações
  concorrentes de Pacientes diferentes — garantido por invariante de unicidade no
  par (Slot, Consulta confirmada), não por lock pessimista.
- **Corrida expiração-vs-submit:** se, no instante do commit, o Slot ainda está
  *reservado* pelo próprio Paciente (ou *livre* e sem outro vencedor), a
  Confirmação **vence** e cria a Consulta; caso outro Paciente já tenha assumido o
  Slot, a Confirmação retorna 409 com oferta de entrada na Fila de Espera.
  `[ASSUMPTION]`
- Repetir a Confirmação com a mesma `Idempotency-Key` retorna a **mesma** Consulta
  (mesmo id, mesmo corpo), sem criar outra e sem alterar timestamps.
- Repetir a Confirmação com `Idempotency-Key` **diferente** quando já existe
  Consulta para aquele Paciente naquele Slot também retorna a Consulta existente
  (não um erro) — a chave protege o transporte, a invariante de negócio protege o
  domínio. `[ASSUMPTION]`
- Confirmar sem Soft Lock ativo (nunca criado, ou já expirado e Slot retomado)
  retorna 409.

### 4.5 Cancelamento e Penalidade

**Descrição:** O Paciente cancela uma Consulta *confirmada*; o Slot é liberado e
a Fila de Espera avança. Cancelamento com menos de 2 horas de antecedência do
início do Slot registra uma Penalidade de 50% do Valor de Referência, com estado
*devida*. O sistema **registra** a Penalidade; não a cobra. Realiza UJ-2.

**Requisitos Funcionais:**

#### FR-8: Cancelar Consulta

O Paciente pode cancelar sua Consulta *confirmada*; a Consulta passa a *cancelada*
e o Slot volta a *livre*, acionando o avanço da Fila de Espera (FR-13).

**Consequências (testáveis):**
- Cancelar é idempotente: repetir com a mesma `Idempotency-Key`, ou recancelar uma
  Consulta já *cancelada*, retorna o mesmo resultado sem segundo avanço de fila e
  sem segunda Penalidade.
- Cancelar Consulta *concluída* ou *no-show* retorna 409.
- O Slot liberado fica imediatamente elegível para Soft Lock por outro Paciente ou
  para Oferta à Fila de Espera.

#### FR-9: Registrar Penalidade por cancelamento tardio

Ao cancelar com menos de 2 h (em UTC) do início do Slot, o Sistema cria uma
Penalidade de 50% do Valor de Referência do Slot, no estado *devida*, vinculada
ao Paciente e à Consulta.

**Consequências (testáveis):**
- Cancelamento com antecedência ≥ 2 h não gera Penalidade.
- O cálculo dos 2 h usa o início do Slot e o instante do cancelamento, ambos em
  UTC — nenhum resultado depende do fuso do cliente (limite testado em ±1 s da
  fronteira).
- Uma Consulta produz no máximo uma Penalidade, mesmo sob cancelamentos
  concorrentes/repetidos.
- O valor da Penalidade é 50% do Valor de Referência **no instante da criação do
  Slot**, não um valor relido depois. `[ASSUMPTION]`

**Fora de Escopo:**
- Cobrança, faturamento, estorno ou disputa da Penalidade.
- Isenção de Penalidade por regra clínica (emergência, atestado).

### 4.6 Fila de Espera

**Descrição:** Quando o Slot desejado não está *livre*, o Paciente entra na Fila
de Espera. Quando o Slot é liberado (Cancelamento, Soft Lock expirado, No-show
confirmado), o Sistema cria uma Oferta para o primeiro Paciente elegível, com uma
Janela de Aceite de 30 minutos contada a partir da **notificação enviada**. Não
aceitou na janela → a Oferta expira e passa ao próximo. Realiza UJ-2, UJ-4.

**Requisitos Funcionais:**

#### FR-10: Entrar na Fila de Espera de um Slot

O Paciente pode entrar na Fila de Espera de um Slot que não está *livre*, ou ser
colocado nela automaticamente após um 409 de Soft Lock/Confirmação. Realiza UJ-2.

**Consequências (testáveis):**
- Um Paciente aparece no máximo uma vez na Fila de Espera de um mesmo Slot;
  reentrada com a mesma `Idempotency-Key` ou repetida retorna a posição
  existente.
- A posição inicial respeita prioridade (VIP antes de comum) e, dentro da mesma
  prioridade, ordem de entrada.
- Entrar na Fila de Espera de um Slot em que o Paciente já tem Consulta
  *confirmada* retorna 409.

#### FR-11: Sair da Fila de Espera

O Paciente pode sair da Fila de Espera de um Slot.

**Consequências (testáveis):**
- Sair é idempotente; sair de uma fila em que não está retorna o mesmo resultado
  neutro.
- Sair com uma Oferta *pendente* dirigida a ele marca essa Oferta como *recusada*
  e aciona o avanço ao próximo (FR-13).

#### FR-12: Criar Oferta ao próximo elegível quando um Slot é liberado

Quando um Slot passa a *livre* por Cancelamento (FR-8), Soft Lock expirado (FR-6)
ou No-show confirmado (FR-22), o Sistema cria uma Oferta *pendente* para o
primeiro Paciente elegível da Fila de Espera e registra o instante de
**notificação enviada**. Realiza UJ-2.

**Consequências (testáveis):**
- Existe no máximo uma Oferta *pendente* por Slot a qualquer instante.
- A Janela de Aceite vence exatamente 30 min (UTC) após o instante de notificação
  enviada registrado — não após entrega nem leitura. `[ASSUMPTION]`
- Se a Fila de Espera está vazia, o Slot permanece *livre* e disponível para Soft
  Lock.
- A criação da Oferta é idempotente em relação ao evento de liberação: um
  Cancelamento reprocessado não cria uma segunda Oferta.
- **Liberação e avanço concorrentes:** se um Cancelamento (FR-8) e o avanço de
  uma Oferta expirada (FR-13) disparam ao mesmo tempo para o mesmo Slot, o
  resultado final tem exatamente uma Oferta *pendente* — os dois caminhos
  convergem, nenhum Paciente elegível é pulado nem servido duas vezes.

#### FR-13: Avançar a Fila de Espera ao expirar ou recusar uma Oferta

O Sistema avança a Fila de Espera quando uma Oferta é *expirada* (Janela de
Aceite vencida) ou *recusada* (FR-11), criando a próxima Oferta ou liberando o
Slot se não há mais elegíveis.

**Consequências (testáveis):**
- Uma Oferta cuja Janela de Aceite venceu nunca pode ser aceita depois: a
  tentativa retorna 409.
- O avanço é idempotente: reprocessar a mesma Oferta expirada não pula um Paciente
  nem cria duas Ofertas seguintes.
- Nenhum Paciente elegível é pulado silenciosamente — cada transição (Oferta
  criada, expirada, recusada, aceita, Paciente promovido) gera um evento de
  auditoria (FR-27).
- O atraso entre o vencimento da Janela de Aceite e a próxima Oferta é ≤
  `[ASSUMPTION]` 30 s.

#### FR-14: Aceitar uma Oferta

O Paciente pode aceitar uma Oferta *pendente* dirigida a ele dentro da Janela de
Aceite; o resultado é uma Consulta *confirmada* no Slot. Realiza UJ-2.

**Consequências (testáveis):**
- Aceitar é idempotente: repetir com a mesma `Idempotency-Key` retorna a mesma
  Consulta.
- Aceitar uma Oferta *expirada*, *recusada* ou dirigida a outro Paciente retorna
  409.
- A aceitação respeita a mesma invariante de unicidade da Confirmação (FR-7):
  nenhum Slot com duas Consultas *confirmadas*.

#### FR-15: Fila de Espera por Médico+data *(opcional)*

`[ASSUMPTION]` O Paciente pode entrar numa Fila de Espera de granularidade
Médico+data (qualquer Slot daquele Médico naquele dia), não apenas de um Slot
específico.

**Consequências (testáveis):**
- Quando qualquer Slot do Médico naquela data é liberado, a Oferta considera
  também os Pacientes da fila Médico+data, mesclados por prioridade e ordem de
  entrada com a fila do Slot específico.
- Um Paciente atendido por uma Oferta é removido de todas as filas em que estava
  para aquele Médico+data.

**Notes:** `[NOTE FOR PM]` Se a granularidade Médico+data ficar fora do MVP, todo
o FR-15 é cortado sem afetar FR-10..FR-14; ver §6.2.

### 4.7 Fila VIP e Anti-starvation

**Descrição:** Pacientes VIP têm prioridade na Fila de Espera. A política
Anti-starvation impede que essa prioridade deixe um Paciente comum para trás
indefinidamente, por dois mecanismos combinados: uma **quota** (após N concessões
consecutivas a VIPs na mesma Fila de Espera, o próximo comum elegível é promovido)
e um **teto de espera** absoluto (um comum aguardando há mais que o teto ganha
prioridade máxima). Realiza UJ-4.

**Requisitos Funcionais:**

#### FR-16: Priorizar Paciente VIP na ordenação da Fila de Espera

A Fila de Espera ordena Pacientes VIP à frente de Pacientes comuns; dentro da
mesma classe, por ordem de entrada. Realiza UJ-4.

**Consequências (testáveis):**
- Um VIP que entra depois de um comum ainda recebe Oferta antes dele, salvo
  aplicação de Anti-starvation (FR-17, FR-18).
- Mudança de classe de um Paciente (comum → VIP ou inverso) reordena a Fila de
  Espera na próxima criação de Oferta, não retroativamente sobre Ofertas já
  emitidas.

#### FR-17: Promover Paciente comum por quota (Anti-starvation)

Após `[ASSUMPTION]` N = 3 concessões de Oferta consecutivas a Pacientes VIP numa
mesma Fila de Espera, o Sistema cria a próxima Oferta para o primeiro Paciente
comum elegível, ignorando VIPs à frente nessa rodada.

**Consequências (testáveis):**
- O contador de concessões consecutivas a VIP zera sempre que um comum recebe
  Oferta (por quota ou naturalmente).
- Com fluxo contínuo de VIPs entrando, um comum na fila recebe Oferta a cada N+1
  liberações, no pior caso.
- N é configurável sem alteração de código.

#### FR-18: Promover Paciente comum por teto de espera (Anti-starvation)

Um Paciente comum que está na Fila de Espera há mais que o `[ASSUMPTION]` teto de
espera (24 h) recebe prioridade máxima: é o próximo a receber Oferta,
independentemente de VIPs à frente.

**Consequências (testáveis):**
- O tempo de espera é medido em UTC desde a entrada na Fila de Espera.
- Se dois comuns ultrapassam o teto, eles são ordenados entre si por ordem de
  entrada.
- Um comum promovido por teto que recusa ou deixa expirar a Oferta retorna à Fila
  de Espera com o mesmo instante de entrada original (não é penalizado), mas o
  contador de quota (FR-17) trata a concessão como feita a um comum.
  `[ASSUMPTION]`
- O teto de espera é configurável sem alteração de código.

**Feature-specific NFRs:**
- A ordenação da Fila de Espera com Anti-starvation aplicada deve ser
  **determinística**: dado o mesmo estado de fila e o mesmo contador, a próxima
  Oferta vai sempre para o mesmo Paciente (sem empate resolvido por acaso).

### 4.8 Lembrete D-1 e Confirmação de Presença

**Descrição:** Um dia antes do Slot, o Sistema envia ao Paciente um Lembrete D-1
com pedido de Confirmação de Presença. Se o Paciente não confirmar presença até
2 h antes do Slot, o Slot é marcado *em risco* e a Fila de Espera recebe uma
**pré-oferta** (aceite condicional) — mas o Slot só é liberado de fato se o
Paciente não comparecer (No-show confirmado, FR-22). Realiza UJ-3.

**Requisitos Funcionais:**

#### FR-19: Enviar Lembrete D-1

O Sistema envia, para cada Consulta *confirmada* cujo Slot começa no dia
seguinte, um Lembrete D-1 com um pedido de Confirmação de Presença.
`[ASSUMPTION]` Disparo às 18:00 no fuso da clínica (convertido para UTC no
agendamento do job).

**Consequências (testáveis):**
- Cada Consulta recebe no máximo um Lembrete D-1; reexecução do job no mesmo dia
  não reenvia.
- Consultas *canceladas*, *concluídas* ou *no-show* não recebem Lembrete D-1.
- O envio passa pela porta de notificação (§10); a implementação de referência
  registra o envio num adaptador de log.

#### FR-20: Registrar Confirmação de Presença

O Paciente pode confirmar presença respondendo ao Lembrete D-1.

**Consequências (testáveis):**
- Confirmar presença é idempotente.
- Confirmar presença após o início do Slot retorna 409 (o caminho válido nesse
  ponto é o Check-in, FR-23).

#### FR-21: Marcar Slot como *em risco* por ausência de Confirmação de Presença

Se, a 2 h (UTC) do início do Slot, não há Confirmação de Presença, o Sistema
marca o Slot como *em risco* e cria uma **pré-oferta** ao primeiro elegível da
Fila de Espera.

**Consequências (testáveis):**
- Um Slot *em risco* ainda tem a Consulta original *confirmada* — o Paciente
  original mantém a prioridade se comparecer.
- A pré-oferta só se converte em Consulta *confirmada* para o Paciente da fila
  **após** No-show confirmado do Paciente original (FR-22).
- Se o Paciente original confirma presença ou faz Check-in enquanto o Slot está
  *em risco*, a pré-oferta passa a *recusada*, o Paciente da fila é notificado de
  que o Slot não abriu, e a transição gera evento de auditoria (FR-27).
- O ciclo é idempotente: reprocessar não cria pré-ofertas duplicadas.

**Notes:** `[NOTE FOR PM]` A pré-oferta condicional é a parte mais delicada do
fluxo de no-show — carrega expectativa emocional dos dois lados. Candidata a
simplificação no MVP (ver §6.2): marcar *em risco* sem pré-oferta e só ofertar
após No-show confirmado.

### 4.9 No-show

**Descrição:** Se o Paciente não faz Check-in até 15 minutos após o início do
Slot, um job do Sistema marca a Consulta como *no-show*. O Médico pode sobrepor:
registrar presença tardia ou desfazer um No-show marcado por engano. Um No-show
confirmado libera o Slot e converte qualquer pré-oferta pendente. Realiza UJ-3.

**Requisitos Funcionais:**

#### FR-22: Marcar No-show automaticamente

O Sistema marca como *no-show* toda Consulta *confirmada* cujo Slot começou há
mais de `[ASSUMPTION]` 15 min sem Check-in registrado.

**Consequências (testáveis):**
- A marcação é idempotente: reexecução não gera segundo evento nem segunda
  liberação de Slot.
- Um No-show confirmado libera o Slot e aciona a conversão de pré-oferta (FR-21)
  ou a criação de Oferta (FR-12).
- Toda marcação registra um evento de auditoria (FR-27) com o instante e a causa
  (ausência de Check-in).

#### FR-23: Registrar Check-in

O Paciente (ou o Médico em seu nome) pode registrar Check-in de `[ASSUMPTION]`
30 min antes do início do Slot até 15 min depois.

**Consequências (testáveis):**
- Check-in dentro da janela impede a marcação automática de No-show.
- Check-in é idempotente.
- Check-in fora da janela retorna 409.

#### FR-24: Sobrepor No-show (Médico)

O Médico dono da Agenda pode desfazer um No-show (paciente chegou atrasado) ou
marcar No-show manualmente antes do prazo automático.

**Consequências (testáveis):**
- Desfazer um No-show devolve a Consulta a *confirmada* e, se o Slot já havia sido
  ofertado/reconfirmado para outro Paciente, a operação retorna 409 com o conflito
  explicitado (não desfaz silenciosamente a Consulta do terceiro). `[ASSUMPTION]`
- Toda sobreposição registra evento de auditoria com o `doctor_id`.
- A operação aceita `Idempotency-Key`.

### 4.10 Idempotência e Automação do Sistema

**Descrição:** Duas garantias transversais materializadas como capacidade
observável. Toda operação de escrita mutante aceita uma Chave de Idempotência e é
segura para repetição. Todos os processos do ator Sistema (expiração de Soft
Lock, avanço de Fila de Espera, Lembrete D-1, marcação de No-show) são jobs
idempotentes e observáveis, sem intervenção humana.

**Requisitos Funcionais:**

#### FR-25: Aceitar e honrar `Idempotency-Key` em toda escrita mutante

Toda rota que cria, altera ou cancela estado (FR-1, FR-2, FR-3, FR-5, FR-7, FR-8,
FR-10, FR-11, FR-14, FR-20, FR-23, FR-24) exige o header `Idempotency-Key` e
retorna a resposta original em replays dentro de 24 h.

**Consequências (testáveis):**
- Requisição mutante sem `Idempotency-Key` retorna 400.
- Replay com a mesma chave e o mesmo corpo retorna status e corpo idênticos aos
  da primeira resposta, sem novo efeito colateral.
- Replay com a mesma chave e corpo **diferente** retorna 422 (conflito de chave).
  `[ASSUMPTION]`
- Após 24 h, a chave é liberada; um replay tardio é tratado como requisição nova e
  é então barrado pelas invariantes de domínio (ex.: FR-7 retorna a Consulta
  existente).
- Duas requisições concorrentes com a mesma chave: uma executa, a outra ou espera
  e retorna a resposta original, ou retorna 409 "em processamento". `[ASSUMPTION]`

#### FR-26: Executar jobs do Sistema de forma idempotente e observável

Os processos do Sistema (FR-6, FR-12, FR-13, FR-17, FR-18, FR-19, FR-21, FR-22)
rodam em intervalos definidos, são seguros para reexecução e emitem métricas e
eventos de auditoria.

**Consequências (testáveis):**
- Rodar qualquer job duas vezes seguidas sobre o mesmo estado produz o mesmo
  resultado final e nenhum evento duplicado.
- Um job interrompido no meio e reexecutado converge ao mesmo estado (sem Slot
  preso em *reservado* por lock órfão, sem Oferta pendente sem dono).
- Cada execução registra: quantos itens processou, quantas transições fez,
  duração.

### 4.11 Observabilidade e Auditoria

**Descrição:** O valor do AgendaFácil é ser **correto e auditável** sob carga.
Toda transição de estado relevante deixa rastro consultável, e o sistema expõe
métricas que permitem verificar as invariantes em teste de contenção.

**Requisitos Funcionais:**

#### FR-27: Registrar trilha de auditoria de transições de estado

Toda transição de Slot, Consulta, Soft Lock, Oferta e Penalidade gera um registro
imutável com: entidade, estado anterior, estado novo, ator (`patient_id` /
`doctor_id` / `system`), causa e timestamp em UTC.

**Consequências (testáveis):**
- Dado um Slot, é possível reconstruir toda a sua história (livre → reservado →
  livre → reservado → confirmado → …) a partir da trilha.
- Nenhuma transição de estado ocorre sem registro correspondente (verificável
  cruzando contagem de eventos com contagem de mudanças).
- Registros de auditoria nunca são atualizados nem apagados.

#### FR-28: Expor métricas de verificação de invariantes

O sistema expõe contadores/consultas que permitem afirmar, a qualquer momento:
zero Slots com duas Consultas *confirmadas*; zero Ofertas *pendentes* duplicadas
por Slot; zero Penalidades duplicadas por Consulta; distribuição de tempo de
espera de Pacientes comuns na Fila de Espera.

**Consequências (testáveis):**
- Após um teste de contenção com 50 requisições concorrentes por Slot em ~1000
  Slots, todos os contadores de violação permanecem em zero.
- A métrica de tempo máximo de espera de Paciente comum é consultável e fica
  dentro do alvo de SM-4 (teto de espera + 10% de margem).

## 5. Não-Objetivos (Explícitos)

- **Não** é um marketplace de médicos nem um sistema multi-clínica com
  descoberta pública — é o motor de agendamento de uma clínica/rede pequena.
- **Não** cobra, fatura, estorna nem concilia a Penalidade — apenas a registra
  como *devida*.
- **Não** faz teleconsulta, prontuário eletrônico, prescrição nem armazena
  qualquer dado clínico do Paciente.
- **Não** entrega frontend, app nativo nem UI web — a entrega é a API.
- **Não** implementa autenticação, cadastro de identidade, gestão de sessão ou
  autorização multi-papel além de distinguir os atores por claim confiável.
- **Não** faz gestão avançada de disponibilidade: recorrência, feriados,
  bloqueios em massa de agenda já confirmada, realocação automática de Pacientes.
- **Não** envia notificações reais (email/SMS/push) — expõe uma porta de
  notificação com adaptador de referência.
- **Não** persegue capacidade/escala de produção — os alvos numéricos são para
  verificação em teste.
- **Não** usa trava pessimista no banco (`SELECT ... FOR UPDATE`) para resolver
  concorrência de Slot — é uma restrição de projeto, não só uma preferência.

## 6. Escopo do MVP

### 6.1 Dentro do Escopo

- Publicação de Agenda: cadastro de Médico, publicação de Slots em lote,
  bloqueio de Slot livre (FR-1..FR-3).
- Descoberta de Slots disponíveis de um Médico (FR-4).
- Soft Lock de 15 min não renovável, com resolução de concorrência sem lock
  pessimista e expiração pelo Sistema (FR-5, FR-6).
- Confirmação de Consulta idempotente, incluindo a corrida expiração-vs-submit
  (FR-7).
- Cancelamento idempotente e registro de Penalidade de 50% para < 2 h (FR-8,
  FR-9).
- Fila de Espera por Slot: entrada/saída, Oferta ao próximo, Janela de Aceite de
  30 min a partir da notificação enviada, avanço automático, aceitação
  (FR-10..FR-14).
- Fila VIP com Anti-starvation por quota (N=3) e teto de espera (24 h)
  (FR-16..FR-18).
- Lembrete D-1 e Confirmação de Presença; marcação de Slot *em risco*
  (FR-19..FR-21).
- No-show automático a 15 min, Check-in, sobreposição pelo Médico (FR-22..FR-24).
- Idempotência de todas as escritas mutantes; jobs do Sistema idempotentes e
  observáveis (FR-25, FR-26).
- Trilha de auditoria e métricas de verificação de invariantes (FR-27, FR-28).
- Tempo em UTC internamente, com offset da clínica só na apresentação.

### 6.2 Fora do Escopo do MVP

- **Fila de Espera por Médico+data (FR-15).** Mantém o modelo de fila mais
  simples (um Slot, uma fila). Deferido para v2. `[NOTE FOR PM]` Se o cliente
  fino precisar de "me avise de qualquer horário desse médico", reavaliar.
- **Pré-oferta condicional no Slot *em risco* (parte de FR-21).** MVP pode
  marcar *em risco* e só criar Oferta após No-show confirmado; a pré-oferta
  condicional entra em v2. `[NOTE FOR PM]` Emocionalmente relevante — revisar se
  o prazo permitir.
- **Bloqueio/realocação de Slot já *confirmado* (FR-3 estendido).** Deferido —
  exige política de realocação em massa.
- **Isenção de Penalidade por regra clínica.** v2.
- **Renovação de Soft Lock.** Fora — decisão de projeto para manter a janela
  previsível.
- **Persistência da resposta idempotente além de 24 h.** Janela fixa no MVP.

## 7. Métricas de Sucesso

*Cada SM cross-referencia os FRs que valida. Alvos são propostas ajustáveis
`[ASSUMPTION]`, escolhidos para serem verificáveis em teste.*

**Primárias**

- **SM-1 — Zero double-booking.** Nº de Slots com duas Consultas *confirmadas*
  após teste de contenção (50 req. concorrentes/Slot, ~1000 Slots). Alvo: **0**.
  Valida FR-5, FR-7, FR-14, FR-28.
- **SM-2 — Idempotência comprovada.** Proporção de replays (mesma
  `Idempotency-Key`) que retornam a resposta original sem efeito colateral novo,
  sobre confirmar/cancelar/aceitar/entrar-na-fila. Alvo: **100%**. Valida FR-7,
  FR-8, FR-14, FR-25.
- **SM-3 — Reaproveitamento de vaga na janela.** Proporção de Slots liberados
  (cancelamento ou lock expirado) com Fila de Espera não vazia que resultam em
  Consulta *confirmada* de um Paciente da fila dentro de uma Janela de Aceite.
  Alvo: **≥ 80%**. Valida FR-12, FR-13, FR-14.
- **SM-4 — Anti-starvation efetivo.** Tempo máximo de espera de um Paciente comum
  na Fila de Espera sob fluxo contínuo de VIPs, em simulação. Alvo: **≤ 24 h +
  10% de margem**. Valida FR-17, FR-18.

**Secundárias**

- **SM-5 — Latência de liberação.** p95 do atraso entre o gatilho (lock vencido,
  Janela de Aceite vencida, No-show) e a transição de estado efetiva. Alvo: **≤
  30 s**. Valida FR-6, FR-13, FR-22, FR-26.
- **SM-6 — Consistência temporal.** Nº de erros de fronteira de janela (15 min /
  30 min / 2 h / D-1 / 24 h) em suíte de testes com clientes em fusos variados.
  Alvo: **0**. Valida FR-9, FR-12, FR-18, FR-19, FR-21, FR-22.
- **SM-7 — Redução de no-show (proxy).** Em simulação com e sem Lembrete D-1 +
  Confirmação de Presença, queda relativa na taxa de Consultas que terminam em
  *no-show*. Alvo: **≥ 20%**. Valida FR-19, FR-20, FR-21. `[ASSUMPTION]` Sem
  dados reais de clínica, medido por modelo de simulação, não em produção.
- **SM-8 — Completude da auditoria.** Diferença entre nº de transições de estado
  e nº de registros de auditoria. Alvo: **0**. Valida FR-27.

**Contra-métricas (não otimizar)**

- **SM-C1 — Tempo de resposta do Soft Lock/Confirmação.** Não otimizar latência
  a ponto de enfraquecer as invariantes de unicidade ou introduzir caminhos que
  contornem a auditoria. Contrabalança SM-1 e SM-5. Uma resposta 20 ms mais
  rápida que perde uma transição de auditoria é uma regressão.
- **SM-C2 — Taxa de aproveitamento de vaga.** Não otimizar SM-3 encurtando a
  Janela de Aceite nem pulando Pacientes elegíveis para "fechar" a vaga mais
  rápido. Contrabalança SM-3. A janela de 30 min e a ordem justa da fila são
  fixas; a métrica serve à justiça, não à ocupação a qualquer custo.
- **SM-C3 — Simplicidade de fila vs. justiça.** Não otimizar SM-4 tornando a
  política Anti-starvation tão agressiva que a prioridade VIP deixe de ter
  efeito prático. Contrabalança SM-4. VIP deve continuar sendo atendido antes na
  maioria dos casos.

## 8. Perguntas em Aberto

1. Granularidade da Fila de Espera: só por Slot (MVP) é suficiente para o cliente
   fino, ou Médico+data (FR-15) é requisito real desde a v1?
2. Pré-oferta condicional (FR-21): entra no MVP ou fica para v2? Impacta a
   complexidade do fluxo de no-show significativamente.
3. Parâmetros Anti-starvation: N=3 e teto=24 h são adequados ao volume esperado?
   Precisam variar por especialidade?
4. Corrida idempotência concorrente (FR-25): a segunda requisição com a mesma
   chave deve **bloquear até a resposta** ou retornar 409 "em processamento"?
5. Valor de Referência: fixado na criação do Slot (posição atual) ou relido do
   Médico no momento do cálculo da Penalidade? Afeta FR-9.
6. Desfazer No-show após a vaga já ter sido reconfirmada por terceiro (FR-24):
   409 é a resposta certa, ou deve haver um fluxo de resolução?
7. Horário de disparo do Lembrete D-1 (18:00 fuso da clínica): configurável por
   clínica? E se a clínica tiver Slots em múltiplos fusos?
8. Bloqueio de Slot já confirmado (cancelamento do médico): mesmo fora do MVP,
   qual o comportamento esperado quando surgir — realocação, cancelamento em
   massa com Penalidade isenta?
9. Autenticação: o claim confiável traz também a classe VIP do Paciente, ou ela é
   consultada internamente? Afeta FR-16.
10. Retenção da Chave de Idempotência: 24 h é suficiente para os padrões de retry
    dos clientes finos previstos?

## 9. Índice de Premissas

*Cada `[ASSUMPTION]` do documento, para confirmação explícita:*

- **§0 / §7** — Alvos numéricos existem para verificação em teste SDD, não
  descrevem capacidade de produção.
- **§2.2 / §10** — Contexto é clínica/rede pequena com agenda publicada por
  Administrador; não marketplace aberto.
- **§2.2 / §10** — Identidade de Paciente e Médico chega ao motor já autenticada,
  como claim confiável (`patient_id` / `doctor_id`); auth, cadastro de identidade
  e sessão fora do escopo.
- **§3 / FR-17 / FR-18** — Anti-starvation: N = 3 concessões VIP consecutivas;
  teto de espera = 24 h; ambos configuráveis sem alteração de código.
- **§3 / FR-19** — Lembrete D-1 disparado às 18:00 no fuso da clínica.
- **§3 / FR-12** — Janela de Aceite de 30 min contada a partir do instante de
  **notificação enviada** registrado pelo Sistema, não de entrega/leitura.
- **§4.3 (FR-5)** — Um Paciente tem no máximo 1 Soft Lock ativo por Médico+data.
- **§4.3 (FR-6) / §4.6 (FR-13)** — Atraso máximo entre vencimento e liberação
  efetiva ≤ 30 s.
- **§4.4 (FR-7)** — Corrida expiração-vs-submit: Confirmação vence se, no commit,
  o Slot está *reservado* pelo próprio Paciente ou *livre* sem outro vencedor;
  senão 409 + oferta de Fila de Espera.
- **§4.4 (FR-7)** — Repetir Confirmação com `Idempotency-Key` diferente, havendo
  já Consulta do Paciente no Slot, retorna a Consulta existente (não erro).
- **§4.5 (FR-9)** — Valor da Penalidade = 50% do Valor de Referência fixado na
  criação do Slot, não relido depois.
- **§4.6 (FR-15)** — Fila de Espera por Médico+data como capacidade opcional.
- **§4.8 (FR-21)** — Pré-oferta condicional no Slot *em risco*, convertida só
  após No-show confirmado.
- **§4.9 (FR-22 / FR-23 / FR-24)** — No-show automático a 15 min sem Check-in;
  janela de Check-in de −30 min a +15 min; desfazer No-show após reconfirmação
  por terceiro retorna 409.
- **§4.10 (FR-25)** — Replay com mesma chave e corpo diferente retorna 422;
  requisições concorrentes com a mesma chave: uma executa, a outra espera ou
  recebe 409 "em processamento".
- **§7 (SM-7)** — Redução de no-show medida por simulação, sem dados reais de
  clínica.
- **§7** — Escala-alvo do teste de contenção: ~50 Médicos, ~1000 Slots/dia, 200
  Pacientes concorrentes, até 50 requisições simultâneas por Slot.
- **§11** — Política de versão: breaking changes só em nova major de rota
  (`/v2/...`); campos novos são aditivos.

---

## 10. NFRs Transversais

*Requisitos não-funcionais de sistema, não presos a uma única feature.*

- **Concorrência sem lock pessimista.** Nenhum caminho de decisão sobre estado de
  Slot pode usar trava pessimista de linha no banco. A resolução de disputa usa
  reserva otimista + invariante de unicidade + expiração. Verificável por revisão
  de código e por ausência de serialização observável sob contenção.
- **Idempotência universal.** Toda operação de escrita mutante é segura para
  repetição (FR-25). Nenhuma exceção sem `[NOTE FOR PM]` explícito.
- **Tempo em UTC.** Todo timestamp persistido e toda aritmética de janela em UTC;
  conversão para fuso local só na serialização de resposta. Nenhuma regra de
  negócio lê o fuso do cliente.
- **Identidade dos atores.** Paciente e Médico chegam autenticados; a identidade
  é um claim confiável (`patient_id` / `doctor_id`). O Sistema é ator interno
  (jobs). Autenticação, sessão e cadastro de identidade estão fora do escopo.
- **Notificações via porta.** O envio de Lembrete D-1 e avisos de Oferta passa
  por uma `NotificationPort`; a implementação de referência usa um adaptador de
  log/fake. Canais reais estão fora do escopo.
- **Observabilidade.** Todo job do Sistema emite métricas (itens processados,
  transições, duração) e todo evento de transição é auditável (FR-27). Métricas
  de verificação de invariantes sempre consultáveis (FR-28).
- **Determinismo da fila.** Dada a mesma Fila de Espera e o mesmo contador
  Anti-starvation, a próxima Oferta é sempre para o mesmo Paciente.
- **Consistência sob falha.** Um job ou requisição interrompido no meio e
  reexecutado converge ao mesmo estado — sem Slot preso em *reservado* por lock
  órfão, sem Oferta *pendente* sem dono, sem Penalidade parcial.
- **Portabilidade de relógio.** As invariantes de unicidade não dependem de os
  relógios de app e banco estarem perfeitamente sincronizados (só a precisão dos
  prazos depende, com tolerância declarada).

## 11. Superfície de API (Contrato Público)

*Forma das capacidades expostas, no nível de contrato — assinatura detalhada e
códigos de erro completos ficam para a especificação de API na arquitetura.*

| Capacidade | Método/Rota (indicativo) | FR |
|---|---|---|
| Cadastrar Médico | `POST /doctors` | FR-1 |
| Publicar Slots em lote | `POST /doctors/{id}/slots:batch` | FR-2 |
| Bloquear / desbloquear Slot | `POST /slots/{id}:block` / `:unblock` | FR-3 |
| Listar Slots de um Médico | `GET /doctors/{id}/slots?from&to` | FR-4 |
| Criar Soft Lock | `POST /slots/{id}/locks` | FR-5 |
| Confirmar Consulta | `POST /slots/{id}/appointment` | FR-7 |
| Cancelar Consulta | `POST /appointments/{id}:cancel` | FR-8, FR-9 |
| Entrar na Fila de Espera | `POST /slots/{id}/waitlist` | FR-10 |
| Sair da Fila de Espera | `DELETE /slots/{id}/waitlist/{patientId}` | FR-11 |
| Aceitar Oferta | `POST /offers/{id}:accept` | FR-14 |
| Confirmar Presença | `POST /appointments/{id}:confirm-presence` | FR-20 |
| Registrar Check-in | `POST /appointments/{id}:check-in` | FR-23 |
| Sobrepor No-show (Médico) | `POST /appointments/{id}:no-show` / `:undo-no-show` | FR-24 |
| Agenda confirmada do Médico | `GET /doctors/{id}/appointments?date` | UJ-3 |
| Trilha de auditoria de uma entidade | `GET /audit?entity&id` | FR-27 |
| Métricas de invariantes | `GET /metrics/invariants` | FR-28 |

- **Header obrigatório em toda escrita:** `Idempotency-Key: <uuid>` (FR-25).
- **Política de versão:** breaking changes só em nova major de rota (`/v2/...`);
  campos novos são aditivos. `[ASSUMPTION]`
- **Formato de erro:** envelope consistente com `code`, `message`, e `conflict`
  (quando 409) descrevendo o estado atual da entidade.

## 12. Restrições e Guardrails

- **Segurança / dados.** Não há dado clínico no sistema. Dados pessoais do
  Paciente ficam no mínimo necessário para agendamento (identificador, classe
  VIP, contato para notificação via porta). Sem PHI, o escopo de conformidade
  (LGPD/HIPAA) é limitado e tratado na arquitetura, não aqui. `[NOTE FOR PM]`
  Confirmar que o contato de notificação não traz o sistema para escopo de dado
  sensível.
- **Custo.** Implementação de referência; sem alvo de custo de infraestrutura.
- **Operação.** Sem SLA de produção. Jobs do Sistema rodam em intervalo
  configurável; o alvo de latência (SM-5) é de teste, não contratual.
