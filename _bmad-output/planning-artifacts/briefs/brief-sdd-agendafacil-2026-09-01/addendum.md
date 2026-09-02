---
title: "Addendum: AgendaFácil"
status: draft
created: 2026-09-01
updated: 2026-09-01
---

# Addendum: AgendaFácil

Detalhe técnico e de regra de negócio que o usuário trouxe no dump inicial.
Não cabe no corpo do brief (executivo, 1–2 páginas), mas alimenta diretamente o
PRD, a arquitetura e a solution design.

## Stack alvo

| Camada        | Tecnologia          |
|---------------|---------------------|
| Linguagem/API | Go + Echo           |
| Persistência  | PostgreSQL          |
| Coordenação   | Redis               |
| Empacotamento | Docker              |

## Restrições arquiteturais (do usuário, verbatim)

- **Sem lock pessimista no banco.** Nada de `SELECT ... FOR UPDATE` segurando a
  linha do slot durante a decisão do paciente. Concorrência resolvida por
  reserva otimista + expiração (candidatos: coluna de versão / `UPDATE ...
  WHERE status = 'free'` com checagem de linhas afetadas; soft lock materializado
  em Redis com TTL; constraint de unicidade no par (slot, status confirmado)).
- **UTC interno.** Todo timestamp persistido e toda aritmética de janela em UTC.
  Conversão para fuso local só na borda de apresentação.
- **Operações idempotentes.** Toda escrita (confirmar, cancelar, aceitar vaga)
  aceita chave de idempotência; repetição retorna o mesmo resultado sem efeito
  colateral novo.

## Atores

| Ator                | Papel                                                          |
|---------------------|---------------------------------------------------------------|
| Paciente            | Escolhe médico/data/slot, confirma, cancela, entra na fila.   |
| Médico              | Dono da agenda; consome a agenda confirmada.                  |
| Sistema (automação) | Expira soft lock, avança fila, dispara lembretes D-1.         |

## Regras críticas — parâmetros a fixar na spec

| Regra                    | Valor conhecido            | Em aberto |
|--------------------------|----------------------------|-----------|
| Soft lock                | 15 min                     | Renovável? Comportamento em expiração durante submit. |
| Janela de aceite (fila)  | 30 min                     | Contagem a partir de quê — notificação enviada ou entregue? |
| Penalidade cancelamento  | 50% se < 2 h de antecedência | Base de cálculo (valor da consulta); quem/como cobra (fora de escopo). |
| Lembrete                 | D-1, link de confirmação   | Horário do disparo; efeito da não-confirmação (libera slot? só marca risco?). |
| Fila VIP anti-starvation | regra existe               | Mecanismo (quota N:1, promoção por idade na fila, teto de espera). |

## Cenários de concorrência a cobrir

- Dois pacientes confirmam o mesmo slot dentro da janela de lock.
- Lock expira no exato instante em que o paciente envia a confirmação.
- Cancelamento e avanço de fila disparam simultaneamente para a mesma vaga.
- Retry de cliente reenvia "confirmar" após timeout de rede.
- VIP entra na fila enquanto um paciente comum está com a janela de 30 min aberta.

## Fora de escopo (registrado)

Pagamento/cobrança, teleconsulta, prontuário eletrônico. A penalidade de
cancelamento é **registrada como devida**, não cobrada.
