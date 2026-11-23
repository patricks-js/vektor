# Project Context — Linux Homelab Manager

**Status:** Fase 1 (Host Monitoring — MVP) implementada  
**Plataforma inicial:** Ubuntu Server  
**Referência principal:** [Beszel](https://github.com/henrygd/beszel)

## 1. Visão

Construir uma plataforma self-hosted, leve e moderna para monitoramento e gerenciamento de servidores Linux, máquinas virtuais e containers, com foco em homelabs.

O projeto nasce como uma ferramenta de monitoramento do próprio host Linux e evolui progressivamente para uma solução integrada de gerenciamento de infraestrutura.

A proposta é combinar a simplicidade e a experiência de monitoramento do Beszel com capacidades de gerenciamento inspiradas no Proxmox, sem incorporar a complexidade de plataformas enterprise.

**Princípio central:** oferecer apenas as funcionalidades necessárias para operar um homelab de maneira simples, eficiente e confiável.

## 2. Objetivos

- Monitorar recursos e saúde do host Linux.
- Disponibilizar métricas em tempo real e histórico de utilização.
- Oferecer alertas configuráveis e notificações.
- Gerenciar recursos e serviços do próprio host.
- Gerenciar máquinas virtuais utilizando KVM/QEMU e libvirt.
- Monitorar e gerenciar containers.
- Oferecer snapshots, backups e recuperação de workloads.
- Disponibilizar uma interface web intuitiva e uma API documentada.
- Manter baixo consumo de recursos e instalação simples.

O desenvolvimento será incremental, priorizando funcionalidades utilizáveis em cada versão.

## 3. Filosofia de engenharia

**Simplicidade acima de abrangência.** Evitar abstrações, dependências e funcionalidades sem necessidade concreta.

**Lightweight by design.** Minimizar consumo de CPU, memória, armazenamento e processos auxiliares.

**Native-first.** Integrar diretamente com as interfaces nativas do Linux sempre que possível.

**Single-node first.** Inicialmente, gerenciar apenas o host onde a aplicação está instalada. Suporte remoto e multi-host são possibilidades futuras.

**Modularidade pragmática.** Separar responsabilidades sem introduzir microserviços ou frameworks complexos prematuramente.

**Segurança e confiabilidade.** Isolar operações privilegiadas, preservar a integridade dos dados e garantir recuperação diante de falhas.

**Código próprio.** Utilizar projetos existentes como referência de pesquisa, não como base de um fork. Preferir bibliotecas consolidadas à reimplementação de funcionalidades de baixo nível.

## 4. Plataforma e stack inicial

| Camada | Tecnologia |
|---|---|
| Sistema operacional | Ubuntu Server 26.04.1 LTS |
| Backend | Go |
| Frontend | React + TypeScript |
| Persistência | SQLite |
| API | REST |
| Atualizações em tempo real | Server-Sent Events |
| Integração Linux | /proc, /sys, D-Bus e APIs nativas |
| Virtualização futura | KVM/QEMU + libvirt |
| Distribuição | Binários nativos + systemd + pacote .deb |

O `vektord` é um monólito modular em execução não privilegiada. A v0.1 já reúne coleta e persistência no mesmo processo. A v0.2 expõe esse pipeline por REST autenticado e SSE sob demanda. Integrações privilegiadas futuras poderão usar um componente separado. [Decisão de processo](docs/adr/ADR-001-process-and-sqlite-architecture.md) · [API da v0.2](docs/API_V0.2.md)

## 5. Primeira fase — Host Monitoring

O primeiro MVP será dedicado ao monitoramento do Ubuntu Server.

**Funcionalidades previstas:**

- Inventário de hardware e informações do sistema.
- Monitoramento de CPU, load average, RAM e swap.
- Monitoramento de discos, filesystems e I/O.
- Monitoramento de interfaces de rede e tráfego.
- Temperaturas e sensores disponíveis.
- Coleta periódica e métricas em tempo real.
- Histórico persistente com agregação e retenção.
- Dashboard web com gráficos e diferentes períodos.
- Alertas básicos para recursos críticos.
- Autenticação administrativa.
- Instalação e execução como serviço nativo.

O MVP não incluirá inicialmente gerenciamento de VMs, containers, alterações avançadas no host ou backups de workloads.

## 6. Evolução planejada

**Fase 1 — Host Monitoring:** métricas, dashboard, histórico e alertas.

**Fase 2 — Host Management:** gerenciamento controlado de serviços, recursos e operações administrativas do Linux.

**Fase 3 — Virtual Machines:** integração com libvirt, gerenciamento do ciclo de vida de VMs, provisionamento e console.

**Fase 4 — Storage & Backups:** volumes, snapshots, backups e restauração de workloads.

**Fase 5 — Containers:** monitoramento e gerenciamento de containers Linux.

As fases representam uma direção de evolução, não um cronograma definitivo.

## 7. Beszel como referência

O Beszel será estudado principalmente por sua implementação de:

- Coletores de métricas Linux.
- Organização e execução do Agent.
- Processamento de métricas e cálculo de taxas.
- Persistência, agregação e retenção de histórico.
- Sistema de alertas.
- Visualização e atualização de gráficos.
- Estratégias de baixo consumo de recursos.
- Instalação e distribuição.

A intenção não é reproduzir sua arquitetura ou código, mas compreender as decisões técnicas, seus benefícios e limitações.

## 8. Diretrizes para o desenvolvimento

1. Não introduzir dependências sem necessidade justificada.
2. Priorizar funcionalidades pequenas, completas e testáveis.
3. Preservar compatibilidade com mecanismos nativos do Linux.
4. Separar monitoramento de operações administrativas privilegiadas.
5. Evitar abstrações criadas apenas para possibilidades futuras.
6. Documentar decisões arquiteturais relevantes.
7. Medir desempenho e consumo de recursos.
8. Tratar segurança, persistência e recuperação como requisitos fundamentais.

## 9. Referência do planejamento inicial

Investigar tecnicamente o Beszel, com foco inicial no motor de métricas e no gerenciamento de histórico.

O resultado dessa investigação deverá orientar a arquitetura, o modelo de dados, os componentes e o plano de implementação do primeiro MVP.

**O projeto deve evoluir a partir de necessidades próprias, usando o Beszel como referência técnica e não como implementação a ser replicada.**
