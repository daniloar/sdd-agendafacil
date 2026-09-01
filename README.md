# sdd-agendafacil

Repositório de apoio da palestra **Spec-Driven Development com AI**. Serve como projeto base para percorrer o [BMAD Method](https://docs.bmad-method.org) do *Project Brief* até o código, usando o **AgendaFácil** (sistema de agendamento de consultas médicas) como exemplo condutor.

O passo a passo completo, com os prompts prontos para colar, está em [`guia-bmad-participantes.md`](guia-bmad-participantes.md).

## O que tem aqui

| Caminho | Conteúdo |
|---|---|
| `guia-bmad-participantes.md` | Guia prático do zero ao código (60–90 min) |
| `_bmad/` | Instalação do BMAD Method (módulos `core` + `bmm`, v6.11.0) |
| `.claude/skills/bmad-*` | Skills do BMAD expostas ao Claude Code |
| `_bmad-output/` | Artefatos de planejamento gerados pelas skills (brief, PRD, arquitetura) |
| `specs/` | Contratos de implementação (`SPEC.md` + `stories.yaml`) |

## Pré-requisitos

```bash
node --version    # 20.12+
python3 --version # 3.10+
uv --version      # gerenciador de pacotes Python
```

Claude Code, editor usado na palestra:

```bash
npm install -g @anthropic-ai/claude-code
```

## Configuração

O idioma já está definido para Português em [`_bmad/bmm/config.yaml`](_bmad/bmm/config.yaml):

```yaml
communication_language: Portuguese
document_output_language: Portuguese
```

Para não commitar preferências pessoais, crie `_bmad/bmm/config.user.yaml` com as chaves que quiser sobrescrever.

## Fluxo da palestra

Cada nível responde a uma pergunta diferente e alimenta o próximo:

| Passo | Skill | Produz | Pergunta que responde |
|---|---|---|---|
| 3 | `bmad-product-brief` | `brief.md` + `addendum.md` | *o quê* — produto e regras de negócio |
| 4 | `bmad-prd` | `prd.md` + `addendum.md` | *por que* — requisitos funcionais e métricas |
| 5 | `bmad-architecture` | `ARCHITECTURE-SPINE.md` | *como* — decisões técnicas (ADRs) |
| 6 | `bmad-spec` | `SPEC.md` + `stories.yaml` | *exatamente o quê e em que ordem* |
| 7 | `bmad-build` | código implementado, testado e revisado | — |

Grand finale: `bmad-party-mode` — o time (Analyst, PM, Architect) delibera sobre uma nova feature antes de qualquer código, e a decisão vira um ADR.

No Claude Code as skills usam prefixo `/`: `/bmad-product-brief`, `/bmad-spec` etc. Se não aparecerem, reinicie o editor após o install. Use `/bmad-help` para inspecionar o estado do projeto e sugerir o próximo passo.

## Hierarquia de artefatos

```
_bmad-output/planning-artifacts/
├── briefs/brief-{projeto}-{data}/
│   ├── brief.md          ← visão geral do produto
│   └── addendum.md       ← profundidade para PRD e Arquitetura
├── prds/prd-{projeto}-{data}/
│   ├── prd.md            ← requisitos funcionais e critérios de aceite
│   └── addendum.md       ← decisões técnicas para o Architect
└── architecture/
    └── ARCHITECTURE-SPINE.md   ← ADRs: contexto, decisão, alternativas, trade-offs

specs/spec-{slug}/
├── SPEC.md        ← contrato de implementação (único arquivo que bmad-build lê)
└── stories.yaml   ← histórias ordenadas com critérios de aceite
```

> Não edite `SPEC.md` à mão. Rode `bmad-spec` de novo — ele atualiza o arquivo mantendo os IDs de capability estáveis.

## O exemplo: AgendaFácil

Sistema de agendamento de consultas médicas usado como caso de estudo.

- **Atores:** Paciente, Médico, Sistema
- **Fluxo:** paciente escolhe médico/data/slot → soft lock de 15 min → confirmação → consulta agendada
- **Regras críticas:** race condition no mesmo slot, fila de espera (janela de 30 min), taxa de cancelamento de 50% com menos de 2h, lembretes D-1, fila VIP com anti-starvation
- **Stack:** Go + Echo, PostgreSQL, Redis, Docker
- **Restrições:** sem lock pessimista no banco, UTC interno, operações idempotentes
- **Fora do escopo:** pagamento, teleconsulta, prontuário eletrônico

## Referências

- Documentação oficial: [docs.bmad-method.org](https://docs.bmad-method.org)
- Repositório do BMAD: [github.com/bmad-code-org/BMAD-METHOD](https://github.com/bmad-code-org/BMAD-METHOD)
- Discord da comunidade: [discord.gg/gk8jAdXWmj](https://discord.gg/gk8jAdXWmj)

---

*Guia preparado para a palestra Spec-Driven Development com AI — Danilo Reis · Staff Software Engineer*
