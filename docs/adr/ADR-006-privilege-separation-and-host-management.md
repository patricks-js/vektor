# ADR-006: Separação de Privilégios e Arquitetura de Host Management

- **Status:** Aprovado
- **Data:** 2026-11-23
- **Contexto:** Fase 2 (Host Management) e preparação para Fases 3–5 (Incus, Storage/Backups e Containers)

## Contexto

Na Fase 1, o `vektord` executa integralmente como usuário sem privilégios (`vektor`), com hardening estrito no `systemd` (`NoNewPrivileges=true`, `CapabilityBoundingSet=`, `ProtectSystem=strict`).

A Fase 2 introduz visibilidade operacional (processos, serviços `systemd` e logs `journald`) e ações administrativas controladas (`start`/`stop`/`restart` de serviços, sinais em processos e energia do host). As fases seguintes adicionarão gerenciamento de VMs e system containers via **Incus**, armazenamento/snapshots/backups e containers.

Executar o servidor HTTP (`vektord`) como `root` ou adicioná-lo a grupos equivalentes a `root` (como `docker` ou `incus-admin`) aumentaria drasticamente o impacto de qualquer vulnerabilidade na camada web.

## Decisão

1. **`vektord` permanece permanentemente sem privilégios.**
   - Operações somente leitura continuam diretamente no `vektord`:
     - Processos via `/proc`.
     - Estado de serviços via `systemd`.
     - Logs recentes via `journald` (concedendo ao usuário `vektor` apenas o grupo `systemd-journal`).
2. **Ações privilegiadas passam por um helper mínimo via Unix Socket (`vektor-ctl.socket`).**
   - Ativado sob demanda via *systemd socket activation* (zero memória ociosa).
   - Escuta em `/run/vektor/ctl.sock` (`0660 root:vektor`).
   - Autentica cada conexão no nível do kernel via `SO_PEERCRED` (`UID == vektor`).
   - Aceita exclusivamente requisições JSON tipadas com **allowlist fechada** de ações (ex.: `service.restart`, `host.reboot`, `process.signal`), sem invocação de shell (`/bin/sh`).
   - Toda mutação é registrada em trilha de auditoria (`audit_log`) no SQLite.
3. **Incus como motor de virtualização nas Fases 3–5.**
   - Em vez de `libvirt` diretamente, o Vektor integrará com o **Incus** (KVM/QEMU + system containers + storage pools/snapshots), reutilizando o mesmo modelo de isolamento de privilégios via `vektor-ctl.socket` quando operações administrativas privilegiadas forem necessárias.

## Plano de entrega incremental

- **v0.5.0:** Leitura operacional (processos `/proc`, serviços `systemd` e logs `journald`) sem alterações de privilégio além do grupo `systemd-journal`.
- **v0.6.0:** Implementação do `vektor-ctl.socket`, ações controladas (`service.*`, `process.signal`, `host.power`), tabela `audit_log` no SQLite e controles com confirmação na UI.
