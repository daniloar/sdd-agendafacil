---
title: "Product Brief: AgendaFácil"
status: draft
created: 2026-09-01
updated: 2026-09-01
---

# Product Brief: AgendaFácil

## Resumo Executivo

O AgendaFácil é um sistema de agendamento de consultas médicas cujo valor está
na **correção sob concorrência**: garantir que um horário nunca seja vendido duas
vezes, que vagas liberadas sejam reaproveitadas de forma justa, e que cada
operação possa ser repetida com segurança sem efeito colateral duplicado.

O problema central não é a tela de calendário — é o que acontece quando dois
pacientes clicam no mesmo slot no mesmo segundo, quando alguém cancela em cima da
hora, ou quando a fila de espera precisa decidir quem entra sem deixar ninguém
para trás indefinidamente. O AgendaFácil trata agendamento como um problema de
consistência distribuída: reserva temporária (soft lock) de 15 minutos,
confirmação explícita, fila de espera com janela de aceite de 30 minutos, fila
VIP com proteção anti-starvation e política de cancelamento com penalidade.

`[ASSUMPTION]` Este brief serve como artefato de referência para
desenvolvimento orientado a especificação (SDD) — uma implementação de
referência para exercitar concorrência, idempotência e regras de negócio bem
definidas, e não um produto comercial com clínica cliente já contratada. O rigor
do brief está calibrado para isso: preciso na mecânica, honesto sobre o que está
fora do escopo, sem inventar diferencial de mercado.

## O Problema

Agendar consulta parece trivial até o sistema ter mais de um usuário ativo ao
mesmo tempo:

- **Double-booking.** Dois pacientes veem o mesmo slot livre e confirmam quase
  simultaneamente. Sem controle de concorrência correto, ambos saem com consulta
  marcada no mesmo horário — o médico descobre na recepção, e um paciente é
  mandado embora.
- **Vaga desperdiçada.** Um paciente cancela. O slot volta a ficar livre, mas
  ninguém é avisado a tempo; o horário fica ocioso enquanto há gente querendo ser
  atendida.
- **Fila injusta.** Quando há fila de espera, encaixes de última hora e regras de
  prioridade (VIP) podem fazer um paciente comum esperar para sempre.
- **No-show.** Paciente esquece, não avisa, não aparece. O horário é perdido para
  todos e não há sinal antecipado para reaproveitá-lo.
- **Operação repetida.** Rede instável, usuário clica "confirmar" duas vezes,
  retry automático do cliente — se cada requisição criar um novo agendamento ou
  cobrar de novo, o paciente é penalizado por um problema de infraestrutura.

Hoje, sistemas simples de agenda resolvem parte disso com trava pessimista no
banco (linha bloqueada até o paciente decidir), o que serializa o atendimento e
não escala. O AgendaFácil parte da restrição oposta: **sem lock pessimista**.

## A Solução

Um fluxo de agendamento com estados explícitos e reservas temporárias:

1. **Descoberta.** Paciente escolhe médico, data e slot a partir da agenda
   publicada.
2. **Soft lock (15 min).** O sistema segura o slot para aquele paciente por 15
   minutos. Outros pacientes veem o slot como indisponível durante a janela. Se a
   confirmação não vier, o lock expira e o slot volta ao pool.
3. **Confirmação.** Paciente confirma → consulta agendada. A operação é
   idempotente: repetir a confirmação não cria uma segunda consulta.
4. **Fila de espera.** Se o slot desejado está ocupado, o paciente entra na fila.
   Quando a vaga abre (cancelamento ou lock expirado), o primeiro elegível recebe
   uma janela de **30 minutos** para aceitar. Não aceitou → passa para o próximo.
5. **Fila VIP com anti-starvation.** Pacientes VIP têm prioridade na fila, mas a
   política garante que pacientes comuns não fiquem indefinidamente para trás
   `[ASSUMPTION]` (ex.: a cada N encaixes VIP, o próximo da fila comum é
   promovido; parâmetro a definir na spec).
6. **Cancelamento.** Cancelamento com menos de 2 horas de antecedência aplica
   **taxa de 50%** `[ASSUMPTION]` (percentual sobre o valor da consulta;
   cobrança em si depende de módulo de pagamento, que está fora de escopo — aqui
   o sistema apenas registra a penalidade devida).
7. **Lembrete D-1.** Um dia antes, o paciente recebe lembrete com link de
   confirmação de presença. Falta de confirmação é sinal antecipado de possível
   no-show e pode acionar a fila de espera `[ASSUMPTION]`.

O ator **Sistema** executa as automações (expiração de lock, avanço de fila,
disparo de lembretes) sem intervenção humana.

## O Que Torna Isto Diferente

Honestamente: não é um diferencial de mercado, é um diferencial de
**engenharia**. O AgendaFácil resolve agendamento concorrente **sem trava
pessimista no banco**, mantendo todas as operações **idempotentes** e o tempo
**em UTC internamente**. O valor é uma base correta sob carga — reserva
otimista, expiração confiável, fila justa e auditável — em vez de uma agenda que
funciona na demo e quebra com dois usuários simultâneos.

## Quem Isto Serve

- **Paciente** — quer marcar consulta rápido, ter certeza de que o horário é
  dele, e entrar numa fila justa quando não há vaga. Sucesso: confirma sem medo
  de perder o slot no meio do caminho, e é avisado a tempo quando uma vaga abre.
- **Médico** — quer a agenda fiel à realidade: sem double-booking, sem paciente
  fantasma, com sinal antecipado de quem provavelmente não vem. Sucesso: chega e
  a agenda do dia está correta.
- **Sistema (automação)** — avança filas, expira locks e dispara lembretes de
  forma idempotente e observável.
- `[ASSUMPTION]` **Administrador da clínica** (secundário) — publica a agenda dos
  médicos e os slots disponíveis. Cadastro e gestão de agenda podem ser mínimos
  nesta versão (seed/config) se o foco for o motor de agendamento.

`[ASSUMPTION]` Contexto de implantação: uma clínica ou rede pequena, com agenda
publicada por um administrador — não um marketplace aberto de médicos.

## Critérios de Sucesso

`[ASSUMPTION]` Todos os alvos abaixo são propostas — ajuste os números.

- **Zero double-booking.** Nenhum slot com duas consultas confirmadas, mesmo sob
  requisições concorrentes (verificável por teste de carga com contenção
  deliberada).
- **Idempotência comprovada.** Repetir qualquer operação de escrita (confirmar,
  cancelar, aceitar vaga) não altera o resultado nem gera efeito duplicado.
- **Reaproveitamento de vaga.** Vaga liberada é oferecida ao próximo elegível em
  segundos; percentual alvo de vagas reaproveitadas dentro da janela de 30 min.
- **Anti-starvation efetivo.** Tempo máximo de espera de um paciente comum na
  fila permanece limitado mesmo com fluxo contínuo de VIPs.
- **Redução de no-show.** Queda mensurável de faltas após introdução do lembrete
  D-1 com confirmação de presença.
- **Consistência temporal.** Nenhum bug de fuso: todos os cálculos de janela
  (15 min, 30 min, 2 h, D-1) corretos independentemente do fuso do cliente.

## Escopo

**Dentro (primeira versão):**

- Fluxo de agendamento com soft lock de 15 min e confirmação explícita.
- Resolução de concorrência sem lock pessimista (reserva otimista + expiração).
- Fila de espera com janela de aceite de 30 min e avanço automático.
- Fila VIP com regra anti-starvation.
- Cancelamento com registro de penalidade de 50% para <2 h de antecedência.
- Lembretes D-1 com link de confirmação de presença.
- Operações idempotentes; horário interno em UTC.

**Fora (explicitamente):**

- Pagamento / cobrança (a penalidade é registrada, não cobrada).
- Teleconsulta.
- Prontuário eletrônico.
- `[ASSUMPTION]` Frontend rico / app nativo — o núcleo é o backend (API); a
  interface do paciente pode ser um cliente fino ou ficar para depois.
- `[ASSUMPTION]` Gestão avançada de cadastro de médicos e disponibilidade
  (assumido mínimo nesta versão).

## Visão

Se o núcleo de agendamento concorrente estiver sólido e auditável, ele vira a
fundação sobre a qual módulos de maior risco podem ser acrescentados com
segurança — pagamento e cobrança da penalidade, teleconsulta, integração com
prontuário, políticas de fila por especialidade, múltiplas unidades. A aposta é
que acertar a consistência primeiro torna todo o resto incremental em vez de uma
reescrita.
