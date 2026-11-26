# Changelog

Todas as mudanças notáveis do projeto **Vektor** são documentadas neste arquivo.

O formato segue [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e o projeto adota [Versionamento Semântico (SemVer)](https://semver.org/lang/pt-BR/).

## [Unreleased]

## [0.9.2] - 2026-11-26

### Added
- **Atualização via CLI (`sudo vektord upgrade`):** novo subcomando no estilo Bun para atualizar o Vektor diretamente pelo terminal (`--check`, `--version`, `--dry-run`, `--yes`/`-y`, `--force`, `--no-backup`, `--data-dir`).
  - Detecção automática do método de instalação (`.deb` via `dpkg-query`/`dpkg -i` ou tarball estático com substituição atômica de `vektord` e `vektor-ctl` e rollback automático se o serviço falhar ao reiniciar).
  - Verificação obrigatória de integridade contra `SHA256SUMS.txt` da GitHub Release e validação de hosts de download confiáveis.
  - Backup online automático do banco SQLite (`VACUUM INTO`) em `vektor.db.pre-<versão>.bak` antes de aplicar a atualização.
- **Dica de atualização na UI (`Settings`):** banner de nova versão agora exibe o comando `sudo vektord upgrade` junto ao link para as notas da release.
- **Pacote compartilhado `internal/update`:** lógica de SemVer e consulta a GitHub Releases unificada entre `GET /api/v1/update` e o subcomando `vektord upgrade`, com cobertura de testes unitários (`internal/update/upgrader_test.go`).

## [0.9.1] - 2026-11-25

### Fixed
- **Vazamento de sockets no `vektor-ctl`:** inicializado um único `*incus.Client` compartilhado no startup (`cmd/vektor-ctl/main.go` e `internal/ctl/server.go`), eliminando criação de `http.Transport` por requisição.
- **Hardening de SSRF no Incus e em Webhooks:**
  - `ValidImageServer` agora exige `https://` público e bloqueia `localhost`, loopback, IPs privados e link-local tanto no `httpapi` quanto no `internal/incus`.
  - `ValidateWebhookURL` bloqueia IPs link-local/metadata (`169.254.0.0/16`, `fe80::/10`, `0.0.0.0`) e `Notifier` não segue redirecionamentos HTTP (`http.ErrUseLastResponse`).
- **Isolamento do pool HTTP para operações longas:** criação de instâncias/backups e streaming de exportação agora usam canal próprio (`longOps`, 4 slots) em vez de disputar o pool de 8 slots (`requests`) das leituras do dashboard.
- **Diagnósticos em paralelo (`GET /api/v1/diagnostics`):** os 6 checks agora executam concorrentemente com `sync.WaitGroup`, evitando timeouts de 8s quando serviços do host estão degradados.
- **Validação estrita de portas em `ValidProxyAddr`:** substituído regex por `net.SplitHostPort` + `netip.ParseAddr` + faixa `1–65535`.
- **Fail-fast no `--allow-user` do `vektor-ctl`:** erro de `user.Lookup` agora aborta o startup com mensagem clara em vez de iniciar silenciosamente apenas com UID 0.
- **Coletor Incus amigável ao `--idle-timeout`:** intervalo mínimo de 30s (alinhado ao bucket raw) e backoff de 5 minutos quando o Incus não está disponível no host.
- **Auditoria de download de backups:** `GET /api/v1/incus/instances/{name}/backups/{backup}/export` agora registra a exportação (sucesso/bytes ou erro) em `audit_log`.
- **UI mais segura e limpa:** confirmação via `<Modal>` antes de remover portas proxy ou variáveis de ambiente em `InstanceDetail`, e estado amigável de espera na aba *Métricas* para instâncias recém-criadas (`ChartPanel`).
- **Refatoração & testes:** lógica de erro/auditoria unificada em `runControl` (`actions.go`/`incus.go`) e nova cobertura E2E para `PATCH`, proxies, rejeição de SSRF e streaming `DoStream` em `tests/integration/incus_http_test.go`.

## [0.9.0] - 2026-11-25

### Added
- **Fase 5 — Incus OCI Nativo (Application Containers):**
  - Detecção automática da extensão `instance_oci` via `/1.0` (`oci_supported` e `oci_count`) com aviso na UI quando o host ainda executa Incus sem suporte OCI.
  - Criação de App Containers OCI direto de registros (`Docker Hub` / `GHCR`) na aba **Instances**, com presets (`Nginx`, `Caddy 2`, `Redis 7`, `Valkey 8`, `PostgreSQL 16` ou imagem customizada), variáveis de ambiente (`CHAVE=VALOR`) e porta exposta inicial.
  - Badge **`OCI`** e filtro dedicado *Apps OCI* na listagem de instâncias.
- **Port Forwarding (`proxy` devices) & Variáveis de Ambiente:**
  - Endpoints `POST /api/v1/incus/instances/{name}/proxies` e `DELETE .../proxies/{device}`, e suporte a `env` / `unset_env` no `PATCH /api/v1/incus/instances/{name}` via `vektor-ctl.socket` (auditados em `audit_log`).
  - Nova aba **Portas & Env** em `InstanceDetail` para gerenciar proxy devices (`TCP`/`UDP`, `0.0.0.0`/`127.0.0.1`) e variáveis `environment.*`.
- **Download de Backups via Streaming Zero-Copy:**
  - Novo suporte a streaming binário no protocolo `vektor-ctl.socket` (`StreamExecutor` + `Client.DoStream`) e endpoint `GET /api/v1/incus/instances/{name}/backups/{backup}/export` com botão **Baixar** (`.tar.gz`) na aba *Backups*.
- **Métricas Históricas (SQLite) por Instância Incus:**
  - Novo tipo de recurso `incus_instance` (`incus:<nome>`), 4 métricas no catálogo (`incus.cpu_pct`, `incus.memory_bytes`, `incus.disk_bytes`, `incus.processes`) e coletor opcional `internal/metrics/collector/incus.go` integrado ao `Scheduler` (rollups `30s`/`5m`/`1h`).
  - Nova aba **Métricas** em `InstanceDetail` com seletor de janela (`15m`–`30d`) e gráficos históricos `ChartPanel`.
- **Modo Demo atualizado** com containers OCI, proxies, env, export de backup e séries históricas simuladas por instância.

### Changed
- **Worker assíncrono de webhooks (`internal/alerts/evaluator.go`):** entregas de webhook agora rodam em goroutine dedicada com fila bufferizada e timeout isolado, impedindo que endpoints lentos bloqueiem o ciclo de avaliação de alertas.
- **OpenAPI sincronizado:** [docs/openapi-v1.yaml](docs/openapi-v1.yaml) atualizado com todas as rotas (`diagnostics`, `update`, `alerts`, `hostmgmt`, `audit` e `incus` completo) e `web/src/lib/api/openapi.d.ts` regenerado.

## [0.8.3] - 2026-11-25

### Added
- **Drawer lateral de diagnóstico no cabeçalho:** clicar no indicador de avisos do `Shell` abre um drawer com os checks em alerta/falha, alternância *Só problemas* / *Ver todos* e atualização sob demanda sem sair da página atual.
- **Visualização inline de logs e cópia de comandos:** cards de diagnóstico (`Overview`, drawer global e painel de indisponibilidade do Incus em `Instances`) ganharam botão **Ver logs** por serviço `systemd` (`incus.service`, `vektor-ctl.service`, `systemd-resolved.service`, `systemd-timesyncd.service`/`chrony.service` e `vektord.service`) com filtro de severidade, além de botão **Copiar** em todos os comandos sugeridos.

## [0.8.2] - 2026-11-25

### Added
- **Motor de diagnóstico acionável (`internal/diagnostics`):** novo endpoint `GET /api/v1/diagnostics` e subcomando CLI `vektord doctor [--json]` com verificações sob demanda para relógio/NTP (`adjtimex` + `systemd-timesyncd`/`chrony`), resolução DNS (`/etc/resolv.conf` e `systemd-resolved`), banco SQLite e coletores, permissão de leitura do `journald`, helper `vektor-ctl.socket` e Incus (diferenciando *não instalado* de serviço `failed`/conflitos com `lxd`/`dnsmasq` + últimas linhas do `journald`).
- **Detalhes de erro por coletor:** `HealthTracker` agora registra `collector_last_errors` (mensagem e timestamp da última falha) e `/api/v1/health` expõe os motivos seguros de `clock_error`.
- **Feedback proativo na UI:** indicador global de avisos no cabeçalho (`Shell`), painel de diagnóstico acionável com botão *Reexecutar* e comandos sugeridos no `Overview`, e exibição da causa real de indisponibilidade do Incus na página `Instances`.

## [0.8.1] - 2026-11-25

### Fixed
- **Concorrência no `vektor-ctl`:** conexões no Unix socket agora são atendidas em goroutines concorrentes (limite de 16 simultâneas) e o `--idle-timeout` aguarda operações em voo concluírem antes de encerrar, evitando bloqueio de polls durante downloads longos de imagens de VM.
- **Timeout para criação de VMs e backups Incus:** `waitOperation` passou a usar `timeout=-1` (governado pelo contexto Go) e os timeouts de `incus.instance.create` e `incus.backup.create` foram ampliados para 10 minutos no `vektor-ctl` e na camada HTTP.
- **Estabilidade do estado do Incus na UI:** erros transitórios em `GET /api/v1/incus/status` retornam HTTP `502` em vez de `200 { available: false }`, evitando que a tela volte falsamente para *"Incus não detectado neste host"* durante operações pesadas.

## [0.8.0] - 2026-11-24

### Added
- **Identidade visual do Vektor:** novas logos (`assets/vektor-banner.png`, `assets/vektor-logo.png`, `assets/vektor-mark*.svg`), `favicon.svg` servido pelo handler embarcado e novo ícone vetorial na marca da barra superior.
- **Fase 4 — Operações Avançadas do Incus:**
  - **Edição de recursos e configuração (`PATCH /api/v1/incus/instances/{name}`):** alteração de vCPUs, RAM (`MiB`/`GiB`), descrição e `boot.autostart` direto na UI, além de barra de progresso de uso de RAM em `InstanceDetail`.
  - **Terminal de comandos (`POST /api/v1/incus/instances/{name}/exec`):** execução de comandos dentro de containers e VMs via Incus REST API com limpeza automática de logs temporários, auditoria em `audit_log`, histórico (`↑`/`↓`) e atalhos rápidos (`uptime`, `free -h`, `df -h`, `ip -br a`, `uname -a`).
  - **Log do Console (`GET /api/v1/incus/instances/{name}/console`):** leitura do buffer de boot/console da instância.
  - **Backups compactados (`GET/POST/DELETE /api/v1/incus/instances/{name}/backups`):** listagem, criação (com opção *Sem snapshots*) e exclusão de backups nativos do Incus.
  - Modo demo atualizado com terminal simulado, console de boot, edição de recursos e backups.

### Fixed
- Ordenação determinística do header HTTP `Allow` em `internal/httpapi/server.go`.

## [0.7.1] - 2026-11-24

### Fixed
- **Permissões do socket `vektor-ctl.socket`:** alterado `DirectoryMode` de `0750` para `0755` em `/run/vektor` e adicionado `chmod 0755 /run/vektor` + `systemctl restart vektor-ctl.socket` no `postinst` do pacote `.deb`, permitindo que o usuário `vektor` atravesse o diretório mantendo `ctl.sock` restrito a `0660 root:vektor` + `SO_PEERCRED`.
- **Mensagens reais de erro na UI:** `ApiError` agora preserva `body.message` retornado pela API em vez de exibir apenas o código (`action_failed` / `invalid_parameters`).
- **Compatibilidade com Incus 6.0.x:** `CreateInstance` não depende mais do campo opcional `start` no `POST /1.0/instances`; inicia a instância explicitamente via `ChangeState(..., "start")` após a criação.

### Changed
- **Formulário de criação de instâncias Incus:**
  - Controle rígido de RAM (campo estritamente numérico + seletor travado `MiB`/`GiB`) e CPU (`1–128` vCPUs inteiros).
  - Validação inline por campo (`Nome`, `Alias customizado`, `CPU`, `RAM`) com feedback visual imediato.
  - Botão **Verificar novamente** agora exibe estado `Verificando…` e horário da última checagem; tela de indisponibilidade diferencia falha no `vektor-ctl.socket` de Incus não instalado.

## [0.7.0] - 2026-11-24

### Added
- **Fase 3 — Virtualização e System Containers via Incus (ADR-006):**
  - Cliente puro em Go stdlib (`internal/incus`) comunicando-se via Unix socket (`/var/lib/incus/unix.socket`) com a API REST 1.0 do Incus (zero dependências externas, zero shell).
  - Isolamento de privilégios mantido: todas as operações do Incus trafegam pelo helper `vektor-ctl.socket` com allowlist fechada e trilha de auditoria no SQLite (`audit_log`).
  - Endpoints REST `/api/v1/incus/{status,instances,resources}` e gestão de estado (`start`, `stop`, `force_stop`, `restart`, `freeze`, `unfreeze`) e snapshots (`list`, `create`, `restore`, `delete`).
  - Nova seção **Instances** (`/instances` e `/instances/:name`) com KPIs, filtros, paginação (15/página), criação de Containers (LXC) e VMs (KVM/QEMU), visualização de Storage Pools e Redes, e gerenciador de Snapshots.
  - Modo demo atualizado (`bun run dev:demo`) com containers, VMs, snapshots, storage pool e rede interativos (tree-shaken fora do build de produção).
  - Teste de integração ponta-a-ponta (`tests/integration/incus_http_test.go`).

## [0.6.1] - 2026-11-24

### Fixed
- **Detecção de sincronia de relógio (`RealClock.Synchronized`):** alinhada à lógica do `systemd-timedated` (`maxerror < 16_000_000` µs, ignorando `STA_UNSYNC`), eliminando falsos positivos de *"Relógio degradado"* em servidores usando `systemd-timesyncd`.

### Changed
- **Paginação nas tabelas operacionais do Host:** adicionada paginação (15 itens por página) nas listas de Serviços e Processos em `HostOperations.tsx`.

## [0.6.0] - 2026-11-23

### Added
- **Fase 2 — Host Management & Separação de Privilégios (ADR-006):**
  - Visibilidade operacional somente leitura no `vektord` (`internal/hostmgmt`): processos via `/proc`, serviços `systemd` e logs `journald` (grupo `systemd-journal`).
  - Helper privilegiado sob demanda `vektor-ctl` (`cmd/vektor-ctl`, `internal/ctl`) com *systemd socket activation* (`/run/vektor/ctl.sock`), autenticação `SO_PEERCRED` e allowlist fechada (`service.*`, `process.signal`, `host.reboot`, `host.poweroff`).
  - Trilha de auditoria persistente em SQLite (`migrations/005_audit.sql`, `internal/audit`) e painel de Controle do Host + Auditoria em **Settings**.
  - Alinhamento visual e melhorias na tabela de processos e serviços.

### Fixed
- Correções da auditoria completa do repositório (`/ponytail-audit`):
  - IDs reais de recursos em `AlertsSettings.tsx` (removidos IDs hardcoded).
  - Rotas SPA `/host/services` e `/host/processes` registradas em `web/handler.go`.
  - Instalação do `vektor-ctl` e units no `packaging/install.sh`.
  - `context.WithoutCancel` no registro de auditoria (`recordAudit`).
  - Rastreamento de processos por `(pid, startTime)` com cache de 1,5s e cache de 60s para `list-unit-files`.
  - Tratamento de permissão/stderr no `journalctl` e testes de integração HTTP (`tests/integration/hostmgmt_http_test.go`).

## [0.4.2] - 2026-11-23

### Added
- Verificador de atualizações na UI (`GET /api/v1/update`) consultando GitHub Releases.
- Preferência de fuso horário **UTC / Local** aplicada aos gráficos e timestamps.
- Piso dinâmico no eixo Y dos gráficos de temperatura.

### Changed
- `postinst` do pacote `.deb` executa `systemctl try-restart vektord` automaticamente em upgrades.
- Removida a cauda SSE ao vivo sobreposta aos gráficos históricos (mantendo apenas buckets persistidos limpos).

## [0.4.1] - 2026-11-23

### Changed
- Atualização mais rápida para janelas curtas (`15s` em `15m`/`1h`).

## [0.4.0] - 2026-11-23

### Added
- **Fase 1 completa (Host Monitoring MVP):**
  - Redesign do dashboard inspirado no Beszel (paleta zinc neutra, navbar flutuante, temas claro/escuro, gráficos de área com tooltips flutuantes, tetos dinâmicos no eixo Y e gráfico multi-série de temperaturas de sensores).
  - Modo demo standalone (`bun run --cwd web dev:demo`), removido automaticamente do bundle de produção via tree-shaking.
  - Sistema de alertas e notificações via webhook (`migrations/004_alerts.sql`, `internal/alerts`, REST + UI em Settings).
  - Empacotamento nativo Linux (`systemd`, `packaging/install.sh`, `packaging/build-deb.sh`).
  - Workflows de CI (`.github/workflows/ci.yml`) e Release SemVer (`.github/workflows/release.yml`) gerando `.deb` e tarballs `amd64`/`arm64`.

## [0.1.0] - 2026-11-20

### Added
- Motor inicial de coleta (`vektord`), armazenamento SQLite WAL multi-resolução (`30s`, `5m`, `1h`), autenticação local com sessões opacas, API REST/SSE e dashboard React/StyleX embarcado via `-tags webui`.
