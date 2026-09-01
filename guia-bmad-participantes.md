# Guia Prático: BMAD Method do Zero ao Código
### Do Project Brief à implementação — passo a passo

> Esse guia foi preparado para os participantes da palestra **Spec-Driven Development com AI**.
> Você vai instalar o BMAD Method, percorrer toda a hierarquia de spec até a menor granularidade, implementar uma feature e, como grand finale, ver o time deliberar sobre uma nova feature em Party Mode.
>
> **Tempo estimado:** 60–90 minutos para o fluxo completo.
> **Documentação oficial:** [docs.bmad-method.org](https://docs.bmad-method.org)
> **Repositório:** [github.com/bmad-code-org/BMAD-METHOD](https://github.com/bmad-code-org/BMAD-METHOD)

---

## Pré-requisitos

```bash
node --version    # 20.12 ou superior
python3 --version # 3.10 ou superior
uv --version      # gerenciador de pacotes Python
```

**Instalar o que estiver faltando:**

```bash
# Node.js via nvm:
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.0/install.sh | bash
nvm install 22

# uv:
curl -LsSf https://astral.sh/uv/install.sh | sh
```

Claude Code (editor usado na palestra):

```bash
npm install -g @anthropic-ai/claude-code
```

> **Prefixo `/`:** no Claude Code e alguns outros editores, os skills usam `/bmad-help`, `/bmad-agent-analyst` etc. Se o comando sem `/` não funcionar, experimente com ele.

---

## Passo 1 — Criar o projeto e instalar o BMAD

```bash
mkdir meu-projeto && cd meu-projeto
git init

# Instalar sem prompts interativos, para Claude Code:
npx bmad-method install --yes --modules bmm --tools claude-code
```

Após a instalação, você verá a pasta `_bmad/` criada no projeto.

> 📖 [docs.bmad-method.org/start/install-bmad](https://docs.bmad-method.org/start/install-bmad/)

---

## Passo 2 — Configurar idioma

Edite `_bmad/bmm/config.yaml` e ajuste duas chaves:

```yaml
communication_language: Portuguese       # conversa com os agentes
document_output_language: Portuguese     # artefatos gerados (brief.md, PRD, ADRs)
```

> **Override pessoal:** crie `_bmad/bmm/config.user.yaml` com as mesmas chaves para não commitar a preferência no repo. Entra em vigor na próxima ativação de um skill.

---

## Passo 3 — Project Brief (visão geral do produto)

O Project Brief é o topo da hierarquia. Define os atores, as regras de negócio e as restrições — o mapa que vai guiar todas as decisões técnicas.

```
bmad-product-brief
```

Quando o Analyst (Mary) perguntar o que você quer construir, descreva seu projeto. Exemplo com o AgendaFácil:

```
AgendaFácil — sistema de agendamento de consultas médicas.

Atores: Paciente, Médico, Sistema (automação)

Fluxo principal: paciente escolhe médico, data e slot → sistema aplica
soft lock de 15 min → paciente confirma → consulta agendada.

Regras críticas:
- Race condition: dois pacientes tentando o mesmo slot simultaneamente
- Fila de espera: janela de 30 min para aceitar a vaga
- Cancelamento: taxa de 50% se menos de 2h de antecedência
- Lembretes D-1 com link de confirmação de presença
- Fila VIP com regra anti-starvation

Stack: Go + Echo, PostgreSQL, Redis, Docker
Restrições: sem lock pessimista no banco, UTC interno, operações idempotentes
Fora do escopo: pagamento, teleconsulta, prontuário eletrônico
```

**Artefatos gerados:**
```
_bmad-output/planning-artifacts/briefs/brief-{projeto}-{data}/
├── brief.md      ← o documento principal
└── addendum.md   ← profundidade que vai alimentar o PRD: alternativas rejeitadas,
                     restrições técnicas, dados de dimensionamento
```

> 📖 [docs.bmad-method.org/plan/define-requirements-and-a-specification](https://docs.bmad-method.org/plan/define-requirements-and-a-specification/)

---

## Passo 4 — PRD (requisitos do produto)

Com o Brief aprovado, o PM (John) traduz a visão em requisitos funcionais detalhados, critérios de aceite e métricas de sucesso. O `bmad-prd` lê automaticamente o `brief.md` e o `addendum.md` do passo anterior.

```
bmad-prd
```

Diga que quer criar um PRD. O PM vai conduzir o processo — no modo **Fast path** ele faz uma ou duas perguntas e gera o documento completo com tags `[ASSUMPTION]` para você revisar.

**Artefatos gerados:**
```
_bmad-output/planning-artifacts/prds/prd-{projeto}-{data}/
├── prd.md         ← requisitos funcionais com IDs estáveis, critérios de aceite
├── addendum.md    ← decisões técnicas (alimenta o próximo passo)
└── .memlog.md     ← log interno do skill
```

> 📖 [docs.bmad-method.org/plan/define-requirements-and-a-specification](https://docs.bmad-method.org/plan/define-requirements-and-a-specification/)

---

## Passo 5 — Arquitetura (decisões técnicas)

O Architect (Winston) registra as decisões técnicas fundamentais antes de qualquer código — como ADRs (Architecture Decision Records). Lê o PRD e o Brief.

```
bmad-architecture
```

Se pedir input, descreva as decisões mais importantes para o seu projeto:

```
Decisões chave para o AgendaFácil:
- Por que PostgreSQL e não MongoDB para os slots
- Como implementar o soft lock sem lock pessimista no banco
- Por que Redis para a fila de espera e não RabbitMQ
- Estratégia de idempotência nas operações de agendamento
```

**Artefato gerado:**
```
_bmad-output/planning-artifacts/architecture/
└── ARCHITECTURE-SPINE.md   ← decisões técnicas com ADRs: contexto, decisão,
                               alternativas descartadas e trade-offs aceitos
```

**Por que isso importa:** um dev novo não vai precisar perguntar por que foi escolhido PostgreSQL. Vai ler o ADR — com o contexto, as alternativas descartadas e os trade-offs aceitos.

> 📖 [docs.bmad-method.org/plan/design-ux-and-architecture](https://docs.bmad-method.org/plan/design-ux-and-architecture/)

---

## Passo 6 — Spec + Stories (granularidade mínima)

`bmad-spec` é o contrato que o Build vai ler. Ele condensa toda a entrada em cinco campos: *Why*, *Capabilities* (cada um com intenção e condição de sucesso), *Constraints*, *Non-goals* e *Success signal*.

Após gerar a spec, peça o **Story Breakdown** — ele divide o épico em histórias implementáveis de forma independente.

```
bmad-spec
```

Aponte para os artefatos existentes:

```
Crie a spec para o épico de Agendamento do AgendaFácil.
Fontes: brief.md, prd.md, ARCHITECTURE-SPINE.md

Após gerar a SPEC.md, faça o Story Breakdown.
```

**Artefatos gerados:**
```
specs/spec-agendamento/
├── SPEC.md        ← o contrato: Why, Capabilities, Constraints, Non-goals,
│                    Success signal — é o único arquivo que o bmad-build lê
└── stories.yaml   ← histórias ordenadas, cada uma com critério de aceite
                     e flag de checkpoint (antes/depois da implementação)
```

> **Atenção:** não edite o `SPEC.md` manualmente. Se precisar ajustar, rode `bmad-spec` novamente com a mudança — ele atualiza o arquivo mantendo os IDs de capability estáveis.

> 📖 [docs.bmad-method.org/plan/define-requirements-and-a-specification](https://docs.bmad-method.org/plan/define-requirements-and-a-specification/)

---

## Passo 7 — Implementação (spec vira código)

Com a spec aprovada e as histórias definidas no `stories.yaml`, o Developer (Amelia) implementa história por história — revisão incluída no fluxo.

```
bmad-build
```

Aponte para a primeira história:

```
Implementar a primeira história do specs/spec-agendamento/stories.yaml
```

O `bmad-build` vai:
1. Ler o `SPEC.md` e a história selecionada
2. Implementar o código respeitando a arquitetura documentada
3. Gerar testes alinhados aos critérios de aceite
4. Fazer revisão automática antes de entregar

**O que você vai ver:** código Go com o handler, a query no PostgreSQL (respeitando o soft lock via Redis) e os testes — todos alinhados com as decisões do `ARCHITECTURE-SPINE.md`.

> 📖 [docs.bmad-method.org/build/build-a-change](https://docs.bmad-method.org/build/build-a-change/)

---

## Grand Finale — Party Mode: o time delibera sobre uma nova feature

O processo anterior mostrou o BMAD no fluxo solo. O Party Mode mostra o que acontece quando uma nova feature chega **depois** que o projeto já tem contexto documentado — com o time inteiro respondendo ao mesmo tempo.

```
bmad-party-mode
```

Após carregar as personas, envie o feature request:

```
Feature request para o AgendaFácil: quero adicionar um sistema de
pontos de fidelidade para os pacientes. Cada consulta confirmada
gera 10 pontos. Acúmulo de 100 pontos = 1 consulta gratuita.

Contexto: o projeto já tem Brief, PRD e Arquitetura documentados.
O stack é Go + PostgreSQL + Redis.

Respondam como time. Não precisam concordar.
Ao responder, cada agente deve se identificar no formato: **Nome (Função):**
```

**O que esperar:**

- 🔍 **Mary (Analyst)** vai questionar se pontos de fidelidade resolvem o problema certo — ou se existe outra alavanca com mais impacto
- 🎯 **John (PM)** vai definir escopo, critério de aceite e prazo estimado
- 🏗️ **Winston (Architect)** vai identificar os riscos técnicos que o PM não considerou: nova tabela, consistência eventual, o que acontece se a consulta for cancelada depois de contabilizar os pontos

O conflito entre as personas é o ponto. Ele acontece **antes de qualquer código**.

---

## Passo Final — Convergência: a decisão vira artefato

```
John e Winston: decidam. Qual é a solução final?
Registrem como ADR:
- Contexto: o que foi debatido
- Decisão: o que foi escolhido e por quê
- Alternativas descartadas: o que foi rejeitado e por quê
- Consequências: trade-offs aceitos
```

O resultado é um **ADR deliberado** — uma decisão que sobreviveu ao escrutínio de três perspectivas. Esse documento entra no repositório junto com o código.

---

## Hierarquia de artefatos gerados

```
_bmad-output/
└── planning-artifacts/
    ├── briefs/
    │   └── brief-{projeto}-{data}/
    │       ├── brief.md          ← visão geral do produto
    │       └── addendum.md       ← profundidade para PRD e Arquitetura
    ├── prds/
    │   └── prd-{projeto}-{data}/
    │       ├── prd.md            ← requisitos funcionais e critérios de aceite
    │       └── addendum.md       ← decisões técnicas para o Architect
    └── architecture/
        └── ARCHITECTURE-SPINE.md ← ADRs: decisões técnicas com raciocínio

specs/
└── spec-{slug}/
    ├── SPEC.md        ← contrato de implementação (o que bmad-build lê)
    └── stories.yaml   ← histórias ordenadas com critérios de aceite
```

Cada nível responde a uma pergunta diferente:
- **Brief** → *o quê* (produto e regras de negócio)
- **PRD** → *por que* (requisitos funcionais e métricas)
- **Architecture** → *como* (decisões técnicas)
- **SPEC + stories** → *exatamente o quê e em que ordem* (contrato de implementação)

---

## Próximos passos

- **[Choose a Planning Path](https://docs.bmad-method.org/plan/choose-a-planning-path/)** — quanto planejamento cada tipo de mudança precisa
- **[Build a Change](https://docs.bmad-method.org/build/build-a-change/)** — como o `bmad-build` funciona em detalhe
- **[Finish an Epic](https://docs.bmad-method.org/build/finish-an-epic/)** — `bmad-retrospective` ao final do épico
- **[Start in an Existing Codebase](https://docs.bmad-method.org/existing-codebases/start-in-an-existing-codebase/)** — adicionar o BMAD a um projeto que já existe
- **[Run Multi-Agent Discussions](https://docs.bmad-method.org/customize/run-multi-agent-discussions/)** — aprofundar o Party Mode
- **[Discord da comunidade](https://discord.gg/gk8jAdXWmj)** — suporte e exemplos

---

## Referência rápida dos skills

| Skill | Produz |
|---|---|
| `bmad-product-brief` | `brief.md` + `addendum.md` |
| `bmad-prd` | `prd.md` + `addendum.md` |
| `bmad-architecture` | `ARCHITECTURE-SPINE.md` |
| `bmad-spec` | `SPEC.md` + `stories.yaml` (via Story Breakdown) |
| `bmad-build` | código implementado, revisado e testado |
| `bmad-party-mode` | múltiplas personas deliberando — você conduz |
| `bmad-help` | inspeciona o projeto e sugere o próximo skill |

Agents para conversas abertas (sem artefato fixo):
`bmad-agent-analyst` · `bmad-agent-pm` · `bmad-agent-architect` · `bmad-agent-dev`

> **Prefixo `/`:** no Claude Code use `/bmad-product-brief`, `/bmad-spec` etc. Se não aparecer, reinicie o editor após o install.

---

## Dúvidas?

- **Documentação oficial:** [docs.bmad-method.org](https://docs.bmad-method.org)
- **Repositório:** [github.com/bmad-code-org/BMAD-METHOD](https://github.com/bmad-code-org/BMAD-METHOD) (52k+ ⭐)
- **Discord:** [discord.gg/gk8jAdXWmj](https://discord.gg/gk8jAdXWmj)

---

*Guia preparado para a palestra Spec-Driven Development com AI*
*Danilo Reis · Staff Software Engineer*
