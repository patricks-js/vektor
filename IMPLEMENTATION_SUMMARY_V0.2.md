# Vektor v0.2 — implementação e evidências

A v0.2 integra HTTP REST, autenticação administrativa e Server-Sent Events ao `vektord`. O daemon continua um processo Go e usa o SQLite existente. A validação automatizada foi executada em CachyOS Linux x86_64 com kernel 6.18.60-2-cachyos-lts, como usuário sem privilégios. O aceite manual no Ubuntu Server 26.04.1 LTS está pendente. [Roteiro de validação](MANUAL_TESTS_V0.2.md)

## Arquitetura implementada

- `cmd/vektord` inicia o listener e o scheduler no mesmo ciclo de vida. `SIGTERM` e `SIGINT` cancelam as requisições, encerram SSE e liberam a porta. O modo `--check` continua sendo uma operação local de diagnóstico.
- `internal/httpapi` atende as nove rotas com `net/http`, bind padrão em `127.0.0.1:8080` e TLS configurável. A capacidade para REST e SSE é limitada separadamente. A escrita SSE tem prazo por evento e o heartbeat é periódico. [ADR-004](docs/adr/ADR-004-http-api-and-realtime.md)
- `internal/auth` usa a migração 003 para o cadastro singleton e os hashes de sessão, apenas no SQLite. O bootstrap é local, o login deriva Argon2id conforme o RFC 9106 e o logout valida origem e CSRF. A conta usa senha definida no bootstrap. O CLI permite revogar todas as sessões. [ADR-005](docs/adr/ADR-005-local-administrator-and-sessions.md)
- O QueryService serve as séries históricas com a escolha de resolução, alinhamento, gaps e cobertura da v0.1. Os recursos usam o ID persistente do registry e paginação. O HTTP acrescenta validações de tipo, timestamps, tamanho de janela e orçamento de pontos.
- O LiveHub continua publicando um snapshot compartilhado por ciclo e ativa a coleta quando há assinantes. O perfil persistente e suas regras de escrita, rollups, reparos tardios, retenção e confiança de relógio preservam o comportamento anterior.

## Contrato HTTP

| Rota | Contrato |
| --- | --- |
| `GET /api/v1/host` | Inventário cacheado e uptime de `CLOCK_BOOTTIME` |
| `GET /api/v1/resources` | Filtros de tipo e estado, cursor por ID |
| `GET /api/v1/resources/{id}` | Detalhe de recurso ativo ou inativo |
| `GET /api/v1/metrics` | Série histórica com resolução e cobertura por bucket |
| `GET /api/v1/health` | Diagnóstico autenticado, com erros do banco sanitizados |
| `GET /api/v1/stream/live` | Eventos `snapshot` em JSON, heartbeat a cada 15 s |
| `POST /api/v1/auth/login` | Inicia sessão e emite cookie e prova CSRF |
| `POST /api/v1/auth/logout` | Revoga a sessão atual, com prova CSRF |
| `GET /api/v1/auth/me` | Administrador, expiração e prova CSRF da sessão |

As operações privadas usam o cookie `vektor_session` com `HttpOnly`, `SameSite=Strict`, caminho `/api/v1` e `Secure` em HTTPS. A duração padrão é 24 horas. Cookies já emitidos continuam válidos após restart, até revogação ou vencimento. As consultas de métricas aceitam 90 dias por requisição e de 1 a 2.000 pontos. Erros da API carregam `code` e `message`. [OpenAPI](docs/openapi-v1.yaml) · [Guia de API, curl, TLS e atualização](docs/API_V0.2.md)

## Comandos e resultados

| Comando | Resultado observado |
| --- | --- |
| `go vet ./...` | Código de saída 0 |
| `go test -count=1 ./...` | Código de saída 0. Suítes unitárias e de integração passaram |
| `go test -race -count=1 ./...` | Código de saída 0, sem condições de corrida reportadas |
| `go build` e smoke test HTTPS local | Binário de produção com TLS direto, cookie seguro e SSE autenticado. Health `ok`, SQLite respondeu, e `SIGTERM` encerrou o stream e o processo com código 0 |
| `go test -race -count=3 ./tests/integration -run '^TestHTTPSSENonReadingClient$'` | Três execuções passaram com escrita persistente avançando durante o bloqueio de um cliente SSE |
| `npx --offline --yes @redocly/cli lint --extends=minimal docs/openapi-v1.yaml` | Especificação validada sem avisos |
| Conferência de rotas e `bash -n` | Os nove métodos e caminhos registrados coincidem com o OpenAPI e os onze blocos Bash dos guias passaram na análise sintática |

## Evidência por critério final

| Critério | Evidência |
| --- | --- |
| Daemon inicia HTTP sem interromper a coleta | `TestDaemonUpgradeRestartAndSignals` verificou binding e respostas. `TestHTTPSSEPersistenceAndLogout` observou avanço do histórico antes, durante e depois do SSE |
| Todos os endpoints definidos estão implementados e documentados | Nove rotas no mux e no OpenAPI, cobertas pelos testes HTTP |
| Administrador funciona sem credenciais padrão | `TestHTTPBootstrapConcurrent` recebeu `401` antes do bootstrap, confirmou ausência de rota de bootstrap e só uma criação entre duas chamadas simultâneas. `TestDaemonUpgradeRestartAndSignals` executou o bootstrap pela CLI |
| Endpoints privados rejeitam clientes sem sessão | `TestHTTPRESTInventoryAndResources` confirmou `401` para host, recursos, detalhe, métricas, health, SSE e auth/me |
| Sessões sobrevivem a restart e podem ser revogadas | `TestDaemonUpgradeRestartAndSignals` reutilizou o cookie e revogou via CLI. `TestHTTPSessionsOriginCSRFExpiryAndRevocation` verificou logout, expiração, remoção de expiradas, digest armazenado e revogação |
| Login oferece proteção contra brute force | `TestHTTPLoginRateLimitAndPayload` confirmou `429`, `Retry-After`, limite de corpo e desconsideração de cabeçalhos encaminhados. `TestHTTPConcurrentLogin` testou contenção e conferiu o número de sessões persistidas |
| Operações mutáveis têm proteção CSRF | `TestHTTPSessionsOriginCSRFExpiryAndRevocation` rejeitou token inválido, token de outra sessão, origens externas e mutação cross-site. `TestHTTPLoopbackRejectsForeignHost` confirmou a proteção de Host |
| QueryService preserva a integridade histórica | `TestHTTPHistoricalSeries` cobriu zero, lacunas, cobertura, resolução explícita, seleção automática, ausência de dados e erros de limite. `TestHTTPDatabaseBusyCanRecover` confirmou leitura em WAL durante contenção, `503` na escrita e recuperação |
| SSE reutiliza LiveHub e coleta compartilhada | `TestHTTPSSEOnDemandSharedCollectionAndNoSQLiteWrites` observou zero coletas sem assinantes e o mesmo snapshot, produzido pela primeira coleta, em dois clientes |
| Clientes lentos ou desconectados preservam os demais | `TestHTTPSSENonReadingClient` saturou um socket e verificou descarte, entrega a outro cliente, avanço das escritas persistentes e remoção por prazo. `TestHTTPSSEOnDemandSharedCollectionAndNoSQLiteWrites` passou por doze ciclos de reconexão com checagem de assinantes e goroutines |
| Streaming preserva frequência das escritas | `TestHTTPSSEOnDemandSharedCollectionAndNoSQLiteWrites` confirmou `PRAGMA data_version` estável durante SSE isolado. `TestHTTPSSEPersistenceAndLogout` verificou persistência antes, durante e depois do streaming |
| Graceful shutdown encerra HTTP e SSE | `TestDaemonUpgradeRestartAndSignals` enviou sinais reais, observou EOF no SSE, saída 0 e porta liberada. `TestHTTPSSEHeartbeatAndShutdown` verificou heartbeat HTTP/2 depois do prazo REST e encerramento limpo |
| Daemon mantém histórico após restart | `TestDaemonUpgradeRestartAndSignals` abriu um banco com schema 001/002, aplicou 003 e comparou um bucket da v0.1 antes e depois de reiniciar e reutilizar a porta |
| A suíte completa passa | `go test -count=1 ./...` passou em todos os pacotes |
| Race detector sem condições de corrida | `go test -race -count=1 ./...` passou em todos os pacotes |
| OpenAPI consistente | Redocly validou o documento e a comparação do mux confirmou as nove operações |
| Regressões da v0.1 continuam passando | Testes v0.1 preservados e executados nas duas suítes completas. Veja a matriz abaixo |
| Roteiro para Ubuntu Server 26.04.1 LTS | Roteiro entregue com comandos e resultados esperados. Execução na VM pendente. [Roteiro](MANUAL_TESTS_V0.2.md) |

`TestHTTPCookiesTLSAndProxy` verificou cookies seguros e HSTS em TLS, cookie Secure com origem pública HTTPS, escopo do cookie e ausência de CORS aberto. `TestHTTPSSESessionExpiry` observou EOF ao vencer a sessão. O teste de processo conferiu que senha, cookie e prova CSRF ficaram fora dos logs capturados.

### Regressões da v0.1

| Contrato | Testes executados |
| --- | --- |
| IDs lógicos da CPU e identidade persistente das NICs | `TestUbuntuLVMBridgesBondsSensorsDiscoveryAndNoNetDoubleCount`, `TestSharedDeviceNICsKeepSeparatePersistentIDs`, `TestDiscoveryAddRemoveReAddPreservesHistoryAndTopology` |
| Perfis live e persist, com compartilhamento de leituras simultâneas | `TestProfilesIndependentAndBootIDReset`, `TestOverlappingLiveAndPersistShareFastSample`, `TestLiveHubOnDemandZeroSQLiteWritesAndSlowSubscriberDrop` |
| Rollups idempotentes, reparo de amostras tardias e recuperação | `TestRollupWeightedMathGapsPartialWindowsAndReplay`, `TestLateRawRepairsClosedRollupsBeforeRetention`, `TestLateSampleSurvivesKillAndRestart` |
| Retenção protegida e cobertura por cadência efetiva | `TestRetentionNeverDeletesUnrolledDataAndBatchesSafely`, `TestQueryUsesPopulatedTiersAndEffectiveCoverage` |
| Saltos de relógio e relógio inicial incompatível com histórico | `TestMaintenancePausesWhenWallTimeJumps`, `TestClockTrustAtStartupAndBackwardStep` |
| Descoberta e probes bloqueantes com shutdown cancelável | `TestDiscoveryWaitIsCancellableAndCannotOverlap`, `TestStartupAndShutdownWithStalledSlowProbes` |

## Decisões e limitações

- O diagnóstico exige sessão. SQLite indisponível pode impedir a autenticação e produzir um `503` sanitizado antes da consulta de detalhes.
- O SSE é efêmero. Eventos carregam até 8.192 pontos e indicam truncamento. Clientes lentos podem perder snapshots ou ser desconectados. A camada HTTP altera somente a ordem em que o contador de assinantes é publicado na saúde do LiveHub, para acompanhá-lo sob concorrência.
- O logout sinaliza a revogação aos streams no mesmo processo. A CLI invalida imediatamente novas requisições e streams já ativos revalidam no heartbeat. A janela do rate limiting é mantida em memória e reinicia junto com o daemon.
- O primeiro teste TLS com `httptest` falhou com EOF porque o cliente negociava h2 e o servidor de teste ainda não o anunciava na própria configuração. O fixture foi corrigido e a verificação HTTP/2 passou.
- O aceite manual e medições de operação prolongada no Ubuntu Server 26.04.1 LTS seguem pendentes. O roteiro registra resultados esperados e a comprovação automatizada identifica o ambiente efetivamente testado.
