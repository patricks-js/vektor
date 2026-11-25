# Guia de Desenvolvimento, Versionamento e Distribuição

Este documento define o fluxo de engenharia do **Vektor** para manter consistência arquitetural, qualidade de código e previsibilidade nas releases entre sessões de desenvolvimento.

---

## 1. Princípios Arquiteturais

1. **Nativo e enxuto (Linux-first):**
   - Binários estáticos em Go (`CGO_ENABLED=0`) sem dependências externas pesadas.
   - Frontend React + TypeScript + StyleX compilado pelo Bun e embarcado no binário `vektord` com `-tags webui`.
   - Zero dependência de SDKs massivos (ex.: integração com **Incus** usa cliente HTTP puro sobre `/var/lib/incus/unix.socket`).
2. **Separação estrita de privilégios ([ADR-006](adr/ADR-006-privilege-separation-and-host-management.md)):**
   - `vektord` roda sempre como usuário sem privilégios (`vektor`), com hardening estrito no `systemd`, e **nunca** é adicionado a grupos equivalentes a `root` (como `incus-admin` ou `docker`).
   - Operações privilegiadas (controle de serviços, sinais em processos, reboot/poweroff, gestão de instâncias Incus) passam exclusivamente pelo helper `vektor-ctl` ativado via *systemd socket activation* (`/run/vektor/ctl.sock`, `0660 root:vektor`, validado por `SO_PEERCRED` e allowlist fechada).
   - Toda mutação privilegiada é gravada em `audit_log` no SQLite.
3. **Virtualização baseada em Incus:**
   - As Fases 3–5 utilizam **Incus** (LXC system containers + KVM/QEMU VMs + storage pools/snapshots), não `libvirt` diretamente.
4. **Demo Mode isolado do bundle de produção:**
   - `web/src/lib/demo/mock.ts` é ativado apenas com `VITE_DEMO_MODE=true` (`bun run dev:demo`) e deve permanecer 100% *tree-shaken* fora do build de produção (`web/dist`).

---

## 2. Ambiente Local e Comandos de Dia a Dia

### Desenvolvimento Frontend (com dados simulados)
```bash
bun install --cwd web --frozen-lockfile
bun run --cwd web dev:demo
```

### Desenvolvimento Full-Stack (daemon + Vite proxy)
```bash
# Terminal 1: daemon local em loopback
go run ./cmd/vektord bootstrap-admin --data-dir ./data
go run ./cmd/vektord --data-dir ./data

# Terminal 2: Vite com proxy para /api/v1
bun run --cwd web dev
```

### Build de Produção (binários estáticos)
```bash
bun run --cwd web build
CGO_ENABLED=0 go build -tags webui -trimpath -o ./bin/vektord ./cmd/vektord
CGO_ENABLED=0 go build -trimpath -o ./bin/vektor-ctl ./cmd/vektor-ctl
```

---

## 3. Checklist de Verificação Antes de Commitar

Sempre execute antes de concluir uma feature ou correção:

```bash
# Backend Go
go vet ./...
go test -race -count=1 ./...

# Frontend Web
cd web
bun run typecheck
bun run lint
bun run test
bun run build
cd ..

# Embedding + garantia de tree-shaking do demo mode
go test -tags webui ./web/...
! grep -q "prod-fra-01" web/dist/assets/*.js
```

Para testes E2E com Playwright (quando houver alterações de fluxos críticos):
```bash
cd web
VEKTOR_CHROMIUM_PATH=/usr/bin/google-chrome-stable bun run test:e2e
```

### Regras ao adicionar novas rotas SPA ou endpoints
- Toda nova rota de página no React Router (`web/src/app/App.tsx`) **precisa** ser liberada em `web/handler.go` para evitar `404` ao recarregar a página diretamente no navegador.
- Toda nova mutação via `vektor-ctl` precisa de validação na allowlist em `internal/ctl/protocol.go`, execução em `internal/ctl/server.go`, proteção CSRF/Origin em `internal/httpapi/` e registro em `audit_log`.
- Sempre atualizar o modo demo (`web/src/lib/demo/mock.ts`) junto com novas telas da UI para permitir preview imediato sem depender do servidor.

---

## 4. Versionamento (SemVer) e Changelog

O projeto segue **Semantic Versioning (`vMAJOR.MINOR.PATCH`)**:

- **`PATCH` (`v0.7.x`):** correções de bugs, ajustes de UX/validação, fixes de empacotamento.
- **`MINOR` (`v0.x.0`):** novas fases ou capacidades relevantes (ex.: `v0.4.0` Fase 1, `v0.6.0` Fase 2 Host Management, `v0.7.0` Fase 3 Incus).
- **`MAJOR` (`v1.0.0`):** marco de estabilidade geral de API, banco e operação em produção.

### Regra de atualização de versão
Antes de criar a tag de release:
1. Atualizar `"version"` em `web/package.json` para `X.Y.Z` (sem o prefixo `v`).
2. Mover os itens de `## [Unreleased]` para `## [X.Y.Z] - YYYY-MM-DD` no [`CHANGELOG.md`](../CHANGELOG.md).
3. Commitar usando [Conventional Commits](https://www.conventionalcommits.org/) (`feat(...)`, `fix(...)`, `docs(...)`, `refactor(...)`).

---

## 5. Processo de Release e Distribuição

As releases são automatizadas via GitHub Actions (`.github/workflows/release.yml`) sempre que uma tag `v*` é enviada ao repositório:

1. **Criar tag anotada e publicar:**
   ```bash
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push git@github.com:patricks-js/vektor.git main vX.Y.Z
   ```
   *(Preferir push via SSH `git@github.com:patricks-js/vektor.git`, pois o token HTTPS do `gh` pode não incluir escopo `workflow`.)*

2. **Artefatos gerados automaticamente no GitHub Releases:**
   - Pacotes Debian/Ubuntu: `vektor_X.Y.Z_amd64.deb` e `vektor_X.Y.Z_arm64.deb` (contendo `vektord`, `vektor-ctl`, units `systemd` e `postinst` idempotente).
   - Tarballs estáticos: `vektor_X.Y.Z_linux_amd64.tar.gz` e `vektor_X.Y.Z_linux_arm64.tar.gz`.
   - `checksums.txt` (SHA-256).

3. **Atualização em servidores Ubuntu/Debian (ex.: `plino-server`):**
   ```fish
   curl -fL -o /tmp/vektor_X.Y.Z_amd64.deb https://github.com/patricks-js/vektor/releases/download/vX.Y.Z/vektor_X.Y.Z_amd64.deb
   and sudo dpkg -i /tmp/vektor_X.Y.Z_amd64.deb
   ```
   O `postinst` do `.deb` recarrega o `systemd`, garante as permissões de `/var/lib/vektor`, `/etc/vektor` e `/run/vektor` (`0755`), reinicia `vektor-ctl.socket` e executa `systemctl try-restart vektord` preservando `/etc/vektor/vektord.env` e o banco SQLite.

---

## 6. Roadmap Resumido

- **Fase 1 (`v0.4.x`) — Concluída:** Monitoramento nativo do host (CPU, RAM, disco, rede, sensores), histórico multi-resolução SQLite, alertas/webhooks, UI Beszel-style, checker de updates e `.deb` + CI/CD.
- **Fase 2 (`v0.6.x`) — Concluída:** Host Management (processos, serviços `systemd`, logs `journald`), helper isolado `vektor-ctl.socket`, ações controladas e auditoria em SQLite.
- **Fase 3 (`v0.7.x`) — Concluída:** Virtualização e System Containers via **Incus** (ciclo de vida, criação, snapshots, storage pools e redes).
- **Fase 4 (`v0.8.x`) — Concluída:** Operações avançadas do Incus (terminal de comandos, console de boot, editor de recursos ao vivo, backups compactados, `vektor-ctl` concorrente) e motor de diagnóstico acionável (`vektord doctor` + drawer global com logs inline).
- **Fase 5 (`v0.9.x`) — Concluída:** Containers de aplicação **OCI nativos no Incus** (Docker Hub/GHCR), Port Forwarding (`proxy` devices), variáveis de ambiente, download streaming de backups (`.tar.gz`), métricas históricas por instância no SQLite, worker assíncrono de webhooks e OpenAPI sincronizado.
- **Rumo à `v1.0.0` (planejado):**
  - Storage Volumes customizados no Incus (criar/anexar volumes persistentes a containers/VMs).
  - Console PTY interativo completo (WebSocket) opcional para programas interativos.
  - Auditoria final de segurança, performance e cobertura E2E de estabilização `v1.0.0`.
