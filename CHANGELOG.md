# Changelog

Todas as mudanças notáveis do projeto **Vektor** são documentadas neste arquivo.

O formato segue [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/) e o projeto adota [Versionamento Semântico (SemVer)](https://semver.org/lang/pt-BR/).

## [Unreleased]

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
