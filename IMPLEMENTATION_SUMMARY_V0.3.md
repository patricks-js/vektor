# Vektor v0.3 — Implementação do frontend

A interface consulta a API autenticada da v0.2 e é servida na mesma origem pelo `vektord`. O frontend usa React, TypeScript, Vite e StyleX. Bun executa o desenvolvimento, os testes e o build. A compilação com `-tags webui` incorpora os arquivos estáticos ao binário Go. [Design System v1](VEKTOR_DESIGN_SYSTEM_V1.md) · [Especificação UX](VEKTOR_V0.3_UX_SPEC.md) · [Roteiro de compilação](README.md)

## Arquitetura

| Módulo | Responsabilidade |
| --- | --- |
| `web/src/app` | Rotas, verificação de sessão, shell responsivo e preferências locais |
| `web/src/components/ui`, `web/src/styles` | Componentes reutilizáveis em StyleX, tokens CSS e fontes locais |
| `web/src/features` | Login, Overview, Host por subsistema, Settings e gráficos históricos |
| `web/src/lib/api` | Tipos gerados do OpenAPI, cliente da mesma origem, paginação do inventário e consultas canceláveis em TanStack Query |
| `web/src/lib/realtime` | Uma conexão SSE por sessão na aba, parsing de snapshots, heartbeat, reconexão e assinaturas por métrica |
| `web/*.go`, `internal/httpapi/server.go` | Integração do bundle estático com `vektord`, mantendo o roteamento da API |

Os tipos são reproduzíveis com `bun run --cwd web generate:api`. O catálogo de chaves e os campos internos de `metadata_json` usados na UI foram conferidos com os coletores Go. O cliente limita as consultas REST a quatro simultâneas por sessão na aba. Cada sessão tem cache próprio em memória. Logout, expiração e retorno do cache de navegação desmontam o conteúdo privado, abortam as consultas ativas e encerram o stream. [OpenAPI](docs/openapi-v1.yaml) · [Catálogo de métricas](internal/metrics/catalog.go) · [Design System implementado](docs/WEB_DESIGN_SYSTEM.md)

## Contrato e decisões de UX

- **Sessão.** A API emite o cookie HttpOnly e retorna expiração e CSRF. O cliente mantém o CSRF em memória e envia `X-CSRF-Token` no logout. Login respeita `Retry-After`, e 401 nas rotas privadas, inclusive SSE, leva ao login com destino interno validado. Preferências de período e barra lateral são locais ao navegador.
- **Overview.** Hostname, SO, kernel e uptime vêm do inventário. A saúde apresentada é do daemon e deriva de `/health`. CPU e RAM usam snapshots. Armazenamento usa a montagem `/` quando o inventário a inclui. Rede usa RX e TX agregados pelo backend segundo o ADR-003. [Identidade e agregação](docs/adr/ADR-003-resource-identity-and-network-aggregation.md)
- **Host.** CPU preserva os IDs de cada core, memória usa as definições de RAM e swap do coletor, armazenamento separa mounts de dispositivos de bloco, e rede preserva cada NIC mesmo com PCI compartilhado. Detalhes usam o ID persistente na URL e nos pedidos de série. Sensores exibem chip, canal e temperatura quando disponíveis.
- **SSE.** O fluxo real é lido via Fetch para observar os comentários heartbeat e os estados HTTP. O limite configurável da cadência rápida é de 10 s e o heartbeat é de 15 s. Após 22 s sem medição recente, a leitura fica atrasada. Ao ficar 75 s sem amostra daquela métrica, o valor dá lugar a indisponível com último timestamp. Após 35 s sem frame, a tentativa é abortada e retomada com backoff com jitter. O retorno a live revalida as consultas ativas. 403 encerra tentativas com erro de acesso visível. Truncamento ou avisos marcam o snapshot como parcial.
- **Armazenamento e sensores.** O scheduler publica no SSE apenas métricas rápidas. As leituras de filesystem e sensores usam o valor `last` do bucket de 30 s mais recente na janela de 15 min e mostram o início desse bucket. Limiares conservadores de atraso de 7 min e 6 min, respectivamente, levam em conta a persistência configurável de até 5 min e as cadências dos coletores lentos. Após o dobro do limiar, o valor dá lugar a indisponível. [Scheduler](internal/metrics/scheduler/scheduler.go) · [Configuração de cadências](internal/config/config.go)
- **Histórico.** Os gráficos SVG usam `avg`, timestamps e resolução efetiva do REST. Cada lacuna interrompe o traço. Um aviso identifica buckets com cobertura parcial. O cursor por mouse ou teclado mostra contagem observada, contagem esperada, cobertura e horário absoluto em UTC. São enviados `step=auto` e `max_points=600`, com opção de solicitar 1 h ou mudar a janela quando a API responde `point_limit`. O histórico tem cadência própria de atualização. O período relativo padrão é 1 h, com opção de janela UTC fixa suportada pela API. Filtros e IDs selecionados sobrevivem a recarga, links diretos e navegação do navegador.
- **Distribuição.** O daemon fornece HTML com `no-store` e assets endereçados por hash com cache imutável. A política de conteúdo da SPA limita scripts, estilos, fontes e conexões à própria origem. O build sem a tag continua servindo a API v0.2. A versão apresentada em Settings identifica o frontend. O contrato REST é identificado como v1.

## Evidências de validação

| Critério | Evidência automatizada |
| --- | --- |
| Login e logout reais, proteção, cookies e retorno a deep link | Playwright com `vektord`, conta criada no SQLite e eventos SSE do coletor Linux |
| Credencial inválida, limite de login e sessão expirada | Credencial errada no daemon, resposta 429 e expiração 401 reproduzidas em testes controlados |
| Overview e subsistemas | Leitura real de inventário, CPU histórica e RAM ao vivo, além de fixtures para recursos renomeados, inativos, IDs não contíguos, NICs com PCI compartilhado e sensores ausentes ou com valor zero |
| Períodos e histórico | Testes de retorno pela URL, janela UTC personalizada, `auto`, `point_limit`, cobertura parcial, lacunas, zero e cursor cruzado |
| SSE normal, offline, stale e reconnect | Daemon real para alternar offline e recuperar. Relógio e stream controlados para distinguir stale e reconnect, verificar o limite de uma conexão e confirmar o cancelamento após 401 |
| Estados e falhas de painel | Playwright com loading, vazio, parcial e falhas isoladas de saúde e inventário |
| Layout e acessibilidade automatizada | Chrome em 1440, 1024, 768, 390 e 360 px. Verificação de overflow, bordas compiladas, axe com regras WCAG A/AA até 2.2 e testes de foco nos menus, gráficos, abas, tabelas e confirmação |
| Build estático e integração Go | `bun run build`, `typecheck`, `lint`, Vitest, testes Go com e sem `webui`, suite com race detector e `go vet` |

Nesta execução, os 6 testes Vitest e os 13 testes Playwright passaram. As suítes Go com e sem a tag, a suíte Go com race detector, o lint, o typecheck e o build também passaram. O Playwright usou Chrome 158.0.8127.36 e o Go usou a versão 1.27.4.

As capturas de tela e os resultados de falha do Playwright ficam em `web/test-results/`. [Testes de navegador](web/e2e) · [Roteiro manual](MANUAL_TESTS_V0.3.md)

## Medições locais

O bundle contém 366,82 kB de JavaScript e 14,59 kB de CSS. Com gzip, os tamanhos medidos pelo Vite foram 116,15 kB e 3,96 kB. As duas fontes WOFF2 totalizam 52,52 kB. O build também passou com apenas o executável Bun no `PATH`. O `vektord` compilado com `CGO_ENABLED=0` foi identificado como ELF estático e executou `--check` com `PATH` vazio. O inventário estava disponível e o SQLite retornou `db_ok: true`.

A sessão de 180 s executou com histórico e SSE reais, alternando entre Overview e Host a cada 15 s. O daemon manteve um assinante em todas as amostras, com zero reinícios do stream após a conexão inicial e 12 pedidos de histórico. Após GC, o heap do Chromium variou de 4.027.672 bytes na primeira amostra a 5.525.652 na última. Houve um documento em todas as amostras e 196 a 214 listeners. O carregamento do documento em loopback, medido por `PerformanceNavigationTiming`, foi de 80 ms. A medição de três minutos deixa em aberto o comportamento de uma sessão de duas horas.

~~~bash
VEKTOR_CHROMIUM_PATH=/usr/bin/google-chrome-stable VEKTOR_SOAK_SECONDS=180 bun run --cwd web test:e2e
~~~

## Limitações para o aceite da release

- O backend v0.2 fornece versão do contrato, cobertura por bucket e timestamps de início dos buckets. Versão binária, cobertura global do período, instante exato do `last` persistido e cadências configuradas ficam indisponíveis no contrato web.
- O formato das séries permite que uma janela com retenção expirada e uma janela ainda sem coleta retornem a mesma lista vazia. A UI informa as duas possibilidades e mostra a resolução efetiva.
- A aceitação em Ubuntu Server 26.04.1 LTS, com hardware da VM, leitor de tela, navegador e sessão de duas horas, permanece pendente. As verificações automatizadas foram executadas localmente em Linux CachyOS com Chrome. [Roteiro manual](MANUAL_TESTS_V0.3.md)
