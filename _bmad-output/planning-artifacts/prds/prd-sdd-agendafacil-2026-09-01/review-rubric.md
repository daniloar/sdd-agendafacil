# PRD Quality Review — AgendaFácil

## Overall verdict

PRD forte e pronto como rascunho de topo de cadeia para alimentar arquitetura,
UX de API e criação de histórias. A tese (agendamento = consistência
distribuída; acertar concorrência primeiro torna o resto incremental) é
explícita e todas as features servem a ela. Riscos: densidade alta de itens em
aberto (~31 entre Open Questions, ASSUMPTIONs e NOTE FOR PM) — aceitável para o
stake (implementação de referência, o usuário delegou os parâmetros), mas exige
a triagem do passo 4. Deriva mecânica leve no termo informal "vaga".

## Decision-readiness — strong

Decisões estão como decisões, não como "considerações": "sem lock pessimista" em
§5 é marcado como restrição de projeto, não preferência; "Soft Lock não
renovável" tem o trade-off nomeado (previsibilidade da janela vs. flexibilidade)
em §6.2. As 10 Open Questions são genuinamente abertas — nenhuma é retórica com
resposta na frase seguinte. `[NOTE FOR PM]` aparece em tensões reais (pré-oferta
condicional com carga emocional dos dois lados; escopo de dado sensível do
contato de notificação), não em checkpoints seguros. Contra-métricas (SM-C1..C3)
protegem contra otimização do alvo errado.

### Findings
- **low** Q4/Q5 são ao mesmo tempo posição tomada (ASSUMPTION) e pergunta aberta
  (§8) — correto para fast path, mas confirme na triagem para não arrastar
  ambiguidade para a arquitetura.

## Substance over theater — strong

Sem teatro de persona: as 4 UJs cada uma dirige comportamento de FR (Rita →
idempotência FR-7; Bruno → fila FR-10/12/14; Dr. Alves → no-show/auditoria
FR-22/27; Ivone/Pedro → anti-starvation FR-17/18). Sem teatro de inovação — §1
declara "não é diferencial de mercado, é de engenharia". NFRs são específicos do
produto (aritmética de janela em UTC, ausência de lock pessimista, determinismo
de fila), não boilerplate. A Visão é específica desta aposta e não trocaria de
PRD sem reescrever.

### Findings
_(nenhum)_

## Strategic coherence — strong

Tese explícita e features priorizadas por ela, não por facilidade. As Success
Metrics validam a tese (SM-1 zero double-booking, SM-2 idempotência, SM-4
anti-starvation) em vez de medir atividade. MVP scope kind = problem-solving; a
lógica de corte em §6.2 acompanha (fila por Slot antes de fila Médico+data;
pré-oferta condicional deferida). Contra-métricas presentes.

### Findings
_(nenhum)_

## Done-ness clarity — strong

Todos os 28 FRs têm ao menos uma consequência testável concreta — códigos HTTP,
contagens, limites de tempo, invariantes verificáveis sob contenção. Poucas
frouxidões residuais, todas de baixo impacto.

### Findings
- **low** FR-4 "ou são omitidos, conforme parâmetro da requisição" (§4.2) — o
  parâmetro não é nomeado. *Fix:* nomear o parâmetro (ex.: `?include=all`) ou
  remover a alternativa.
- **low** FR-28 "teto de espera + margem operacional" (§4.11) não quantifica a
  margem; SM-4 quantifica ("+10%"). *Fix:* referenciar SM-4 explicitamente na
  consequência de FR-28.
- **low** FR-21 "o Paciente da fila é notificado de que a vaga não abriu" — sem
  consequência testável sobre a notificação. *Fix:* asserir que a pré-oferta
  passa a estado *expirada*/*recusada* e gera evento de auditoria.

## Scope honesty — adequate

§5 Não-Objetivos faz trabalho real (9 itens afiados). §6.2 traz motivo por item
e `[NOTE FOR PM]` nos emocionalmente relevantes. 17 ASSUMPTIONs indexados; o
roundtrip do índice está quase completo.

### Findings
- **medium** Densidade de itens em aberto ~31 (10 OQ + 17 ASSUMPTION + ~4 NOTE
  FOR PM) para um PRD que vai alimentar arquitetura. Mitigado porque a maioria
  dos ASSUMPTIONs são valores de parâmetro que o usuário delegou explicitamente
  ("aceite as assumptions e sugira"). *Fix:* triagem do passo 4 — separar
  bloqueadores de fase (corrida idempotência concorrente FR-25; valor da
  Penalidade fixado vs. relido FR-9; granularidade da fila FR-15) dos ajustes de
  número (N=3, teto 24h, 18:00, janela de check-in).
- **low** `[ASSUMPTION]` de "Política de versão" em §11 não aparece no Índice de
  Premissas (§9). *Fix:* adicionar a entrada ou remover a tag.

## Downstream usability — adequate

PRD de topo de cadeia (alimenta bmad-ux, bmad-architecture,
bmad-create-epics-and-stories). Glossário presente; IDs FR-1..28 / UJ-1..4 /
SM-1..8+C1..C3 contíguos e únicos; todos os cross-references resolvem (verificado
por varredura). Cada seção se sustenta sozinha via termos do Glossário. Cada UJ
tem protagonista nomeado.

### Findings
- **low** Deriva de Glossário: "vaga" é usado como sinônimo informal de "Slot"
  / "Slot liberado" (título conceitual de SM-3, FR-12, §6.2, §1 Visão). *Fix:*
  substituir por "Slot liberado" onde for entidade, ou registrar "vaga" no
  Glossário como uso coloquial equivalente a Slot liberado.
- **low** "consulta" minúsculo (genérico) vs. "Consulta" (entidade) — ambíguo em
  "Valor monetário da consulta" (def. de Valor de Referência). *Fix:* padronizar
  para "Consulta" onde referir a entidade.

## Shape fit — strong

Produto é um motor de concorrência backend/API com atores multi-stakeholder
(Paciente, Médico) — não operador único. UJs são justificadas e mantidas
enxutas; a estrutura feature/FR domina, o que é adequado a um spec de
capacidade. SMs misturam verificável-por-teste (SM-1,2,6,8) e simulação
(SM-4,7), coerente com "sem produção". Nem super-formalizado nem
sub-formalizado.

### Findings
_(nenhum)_

## Mechanical notes

- **Deriva de Glossário:** "vaga" (informal) vs. "Slot"; "consulta" vs.
  "Consulta". Baixa severidade, corrigível no polish.
- **Continuidade de ID:** limpa — FR-1..28, UJ-1..4, SM-1..8 + SM-C1..C3, sem
  lacunas nem duplicatas; cross-refs todos resolvem.
- **Roundtrip do Índice de Premissas:** uma tag inline (§11 política de versão)
  fora do índice; o resto fecha.
- **Protagonistas de UJ:** todas nomeadas (Rita, Bruno, Dr. Alves, Ivone/Pedro).
- **Seções obrigatórias:** Essential Spine completo + Adapt-In coerente (NFRs
  Transversais, Superfície de API, Restrições/Guardrails). OK.
