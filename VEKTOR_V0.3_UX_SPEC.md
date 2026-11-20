# VEKTOR — UX & Product Specification v0.3

**Nome da entrega:** Vektor Web Dashboard v0.3
**Direção visual vinculada:** `VEKTOR_DESIGN_SYSTEM_V1.md` — Industrial Dark Hybrid
**Status:** Especificação funcional/UX para implementação; **não** afirma que a UI ou os endpoints auxiliares já existam.
**Baseline:** backend v0.2 aprovado; Ubuntu Server 26.04.1 LTS; execução em `vektord` não privilegiado.

## 1. Objetivo e limites

Construir uma interface web rápida, acessível e confiável para observar **um host**: estado do sistema, utilização, recursos monitorados, histórico e streaming live. A v0.3 deve consumir a API v0.2 existente, sem reimplementar coletores, séries ou auth.

**Dentro do escopo:** Login/Logout, Overview, Host (CPU, Memory, Storage, Network, Sensors), Settings enxuto, filtros de período, consultas históricas, SSE, indicadores de saúde/conexão, loading/empty/error, responsividade, testes E2E.

**Fora do escopo:** gerenciamento de VMs e containers, alertas configuráveis, ações root/systemd, operações de host mutáveis, multi-node, RBAC, command palette genérico, temas múltiplos obrigatórios, dashboards customizáveis.

**Precondição do implementador:** inspecionar o OpenAPI e o código reais da v0.2 antes de nomear campos, métricas, estados e recursos. A nomenclatura abaixo é conceitual; o mapeamento exato deve nascer do contrato real. **Não inventar endpoints/campos.** Se algum requisito não for atendido pela API, degradar com honestidade ou relatar uma necessidade de contrato separadamente.

## 2. Arquitetura de navegação

Rotas propostas (adaptáveis à estratégia de roteamento):

- `/login` — autenticação.
- `/` ou `/overview` — visão operacional.
- `/host/cpu`, `/host/memory`, `/host/storage`, `/host/network`, `/host/sensors` — inspeção por subsistema; pode usar abas sob `/host` caso mais simples.
- `/settings` — preferências locais, informações da versão, sessão/logout.

Sidebar: **Overview**, **Host**, **Settings**. Exibir somente páginas funcionais; não adicionar itens inertes de VMs/containers/alerts.

**Navegação persistente:** sidebar expandida/recolhida, indicação da rota atual, contexto de host, conexão e acesso à sessão. No mobile, drawer com abertura por botão, fechamento por Esc e retorno de foco.

**Contexto temporal:** seletor 15m/1h/6h/24h/7d, default 1h (sujeito a faixa efetivamente suportada). Preservar intervalo entre abas de Host e Overview; URL deve representar período selecionado de forma compartilhável quando possível. Voltar/avançar do navegador deve restaurar filtros.

## 3. Modelo de dados da UI

Separar conceitos:

- **Host health:** estado operacional derivado de diagnósticos existentes; nunca inventar alertas.
- **Transport state:** `connecting`, `live`, `reconnecting`, `stale`, `offline` — descreve stream/rede, não a saúde da máquina.
- **Measurement state:** `valid`, `missing`, `unavailable`, `partial`, `stale`; `0` é valor válido, nunca falsy de indisponibilidade.
- **Historical completeness:** cobertura/resolução informadas pelo backend, não estimadas com contagem ingênua de pixels.

**Fontes:** SSE alimenta métricas atuais; REST serve inventário/recursos/histórico. No first load, obter snapshot disponível pela API existente ou sinalizar aguardando primeira leitura; não forçar polling agressivo. Após reconexão, recuperar dados necessários sem criar tempestade de requisições.

**Política de freshness:** definir limiares a partir da cadência real de cada métrica e heartbeat SSE, não apenas um timeout arbitrário igual para tudo. Exibir 'Atualizado há Xs' quando relevante. Congelar último valor somente com rótulo STALE; se ausência prolongada, apresentar indisponível e timestamp da última leitura.

## 4. Tela Login

**Objetivo:** autenticar rapidamente, com aparência de console refinado.

**Layout:** marca Vektor, título curto, identificação do servidor quando disponibilizada sem expor dados sensíveis, campos de usuário/senha conforme contrato, CTA de login, erro inline. Largura contida no desktop; adapta no mobile.

**Interações:** Enter envia; mostrar/ocultar senha; loading desabilita submit duplicado; erro de credenciais não vaza detalhes; rate limit 429 tem mensagem e orientação; foco no primeiro campo/erro; sucesso retorna à rota originalmente solicitada (validada para ser interna).

**Segurança:** cookies de sessão httpOnly gerenciados pelo browser; não guardar tokens em localStorage; CSRF conforme backend; logout invalida sessão e impede acesso à página privada pelo cache/navegação sem nova validação. Tratar 401 global sem loops de redirect.

## 5. Tela Overview

**Pergunta principal:** “Como está o meu servidor agora?”

**Hierarquia:**

1. **Header de host:** hostname, SO/kernel resumidos, uptime e status da conexão/saúde — sem inferir saúde apenas porque SSE está conectado.
2. **KPIs imediatos:** CPU, memória, uso de armazenamento significativo e rede; definir cuidadosamente o significado dos agregados. Se não houver um agregado defensável para disco/rede, substituir pelo recurso principal identificado ou destacar que há múltiplos dispositivos; nunca somar porcentagens incompatíveis.
3. **Atividade temporal:** CPU e memória com série histórica e cursor sincronizado, período compartilhado.
4. **Storage/Network overview:** resumo de filesystems e interfaces em painéis com links para detalhes.
5. **Diagnostics:** falhas de coleta, métricas ausentes e status interno apenas quando fornecidos pela API. Se vazio, indicar “Nenhum problema detectado nos diagnósticos disponíveis”, sem equivaler a sistema 100% saudável.

**Regras:** primeira dobra em desktop deve mostrar identidade, estado e KPIs; sem paredes de gráficos. Valores atuais SSE e histórico REST podem divergir por cadência — legenda/metadados deixam isso claro.

## 6. Tela Host

### CPU

CPU total atual, carga (load averages, sem chamar load de porcentagem), número de CPUs, metadados do processador disponíveis, gráfico histórico e tabela/grade por core. Usar **IDs reais** dos cores; não pressupor sequência contígua. Recursos inativos/ausentes rotulados.

### Memory

RAM usada/disponível/cache/buffers/swap **apenas** de acordo com definições que o backend realmente expõe. Gráfico histórico; não somar métricas incompatíveis nem confundir `available` com `free`. Sem swap: mostrar “não configurado” ou zero respaldado pela API.

### Storage

Separar claramente **filesystems (capacidade)** de **dispositivos de bloco (I/O)**. Tabela com mountpoint, capacidade, uso e disponibilidade quando presentes; tabela de discos/I/O com throughput e operações quando disponíveis. Filtros, busca, ordenação e detalhes individuais com ID persistente. Não apresentar capacidade agregada de mounts sobrepostos como total físico.

### Network

Lista de NICs com nome, identificação persistente, estado (quando presente), receive/transmit, erros/drops e histórico. Taxas em unidade/s; destacar que contadores e taxas são grandezas distintas. Não agregar interfaces com identidades diferentes pela mesma chave PCI.

### Sensors

Sensores descobertos e disponíveis, temperatura/unidade, localização e estado conhecido. Suportar lista vazia sem erro (“Nenhum sensor encontrado”). Distinguir falha de leitura de medição zero. Filtros úteis quando houver volume.

**Padrão comum das abas:** título + contexto + período + resumo + gráfico(s) + tabela/lista detalhada. Manter filtros de tempo ao trocar aba; manter estado da URL e de back/forward.

## 7. Interação e gráficos

- Time range 15m/1h/6h/24h/7d com seleção acessível. Se determinada resolução não puder atender ao `max_points`, mostrar erro compreensível e permitir ampliar step/alterar período, sem fingir dados.
- Hover cruzado entre gráficos com timestamp e valor; interação por teclado e equivalentes acessíveis para detalhes críticos.
- Representar **gaps** como lacunas, nunca linha contínua inventada; valores reais `0` aparecem como zero.
- Label de unidade, eixo, escala temporal e timezone claros; não combinar unidades heterogêneas no mesmo eixo por conveniência.
- Zoom de intervalo opcional na primeira entrega, desde que não prejudique scroll da página/touch; restaurar período óbvio.
- Tooltip deve indicar fonte (live/histórico) quando ambas as camadas aparecem.
- Utilizar downsampling/resolução do backend; não sobrecarregar browser com todos os pontos possíveis.
- `coverage` e `resolution` da API exibidos de forma legível (ex.: rodapé do painel ou menu de detalhes); quando ausentes, ocultar ou indicar ausência, nunca fabricar.
- Estados por painel: skeleton breve, sem dados, sem recurso, dados parciais, erro de API, conexão degradada.

## 8. SSE, reconexão e lifecycle

- Manter apenas uma conexão SSE central por aba do aplicativo; desacoplar distribuição de estado para evitar assinaturas redundantes por card.
- Exponential backoff com jitter e limite razoável se implementação manual for necessária; evitar competir com mecanismo de reconexão nativo do `EventSource`. Fazer autenticação com cookies no mesmo origin ou conforme suporte real.
- No reconnect: estado `RECONNECTING`, conservar último dado com marca STALE e último timestamp; revalidar histórico/recursos quando necessário.
- No 401/403: sair de stream e conduzir ao login/erro adequado, evitando reconexão infinita autenticada incorretamente.
- On logout/unmount: fechar stream, abortar fetches e limpar cache de dados privados em memória.
- Histórico não deve ser reconsultado a cada evento SSE de 2s; atualizações live alimentam apenas áreas atuais adequadas.
- Não prometer a frequência exata do stream até inspecionar backend e configuração real.

## 9. Settings

Inclui preferências puramente de frontend (ex.: sidebar recolhida e período padrão, persistidas localmente sem segredos), informações de versão/backend quando disponíveis e logout.

Não exibir configurações de retenção, agentes, autenticação avançada ou serviços que não tenham API implementada. Qualquer alteração futura exige confirmação adequada, feedback e estados de falha.

## 10. Responsividade e acessibilidade

- Desktop-first, mas totalmente operável em mobile.
- Breakpoints de referência: <768, 768–1023, 1024–1439, ≥1440px.
- Sem overflow horizontal da página; tabelas densas podem ter contêiner com rolagem horizontal identificada.
- Tabs com semântica/teclado; menus com foco gerenciado; labels reais nos inputs; `aria-live` para status importantes sem anunciar cada amostra SSE.
- Atingir WCAG 2.2 AA como meta, incluindo contrastes e foco; não depender apenas de cor.
- Respeitar `prefers-reduced-motion`; não usar efeitos de contagem em KPIs.

## 11. Stack e integração

**Stack preferida:** React + TypeScript + Vite; Tailwind consumindo tokens CSS; primitives acessíveis Radix (se ajudarem); TanStack Query para REST; ícones Lucide; biblioteca de gráficos eficiente (uPlot ou similar validada); Vitest + React Testing Library + Playwright.

**Arquitetura sugerida:**

```text
web/
  src/
    app/              # routing, providers, app shell
    components/ui/    # design system Vektor
    features/auth/
    features/overview/
    features/host/
    features/settings/
    features/metrics/ # charts, time range, transforms
    lib/api/          # cliente tipado, errors, schemas
    lib/realtime/     # SSE central e freshness
    styles/           # tokens e reset
```

Validar política de entrega do web app (assets estáticos servidos pelo próprio daemon ou outro mecanismo) sem adicionar serviços obrigatórios ao runtime. Preferir **mesma origem** para API e SPA, reduzindo complexidade de cookies, CORS e CSRF. Não enfraquecer controles de segurança da v0.2.

## 12. Performance e instrumentação

- Renderizar gráficos de maneira incremental/eficiente; evitar rerender de toda a aplicação a cada mensagem SSE.
- Medir tempo de carregamento, tamanho do bundle, memória de página e comportamento após sessão longa com stream ativo.
- Evitar flashes de dados privados durante troca de sessão.
- Cancelar requisições obsoletas ao trocar período/aba; considerar cache por chave de filtro/ID.
- Falhas parciais devem permanecer isoladas por painel, preservando o restante da página.

## 13. Cenários de teste obrigatórios

1. Login correto, incorreto, 429, expiração de sessão e logout; proteção de rotas privadas.
2. Carregamento Overview com host real, com host parcialmente disponível e com API indisponível.
3. SSE em condição normal; queda; reconexão; stale; logout; sem updates indevidos após unmount.
4. CPU com IDs não contíguos; recursos renomeados/inativos; labels corretos.
5. Disco com múltiplos mountpoints e múltiplos dispositivos; sem soma indevida.
6. Network com várias NICs e taxas distintas; identidade preservada.
7. Sensors vazio, sensor indisponível e sensor com valor zero.
8. Histórico com gaps, cobertura parcial, nível expirado, `step=auto`, erro por `max_points`.
9. Período mantido na navegação; back/forward; link direto e reload.
10. Teclado: login, tabs, menus, filtros, tabelas, foco do modal/drawer.
11. Desktop (1440 e 1024), tablet (768), mobile (390/360): layout, legibilidade e sem overflow global.
12. Sessão longa (ex.: algumas horas) com streaming e troca de abas, verificando vazamento de listeners, conexões e memória.

## 14. Critérios de aceite v0.3 (Definition of Done)

### Funcionalidade

- [ ] Login/logout funcionam contra auth real da v0.2; rotas privadas protegidas.
- [ ] Overview apresenta host, KPIs e diagnósticos corretos conforme dados disponíveis.
- [ ] Host disponibiliza CPU, Memory, Storage, Network e Sensors com conteúdo útil e estados vazios quando aplicável.
- [ ] Identificadores de recursos são preservados; CPU por core não pressupõe IDs consecutivos.
- [ ] Filtros de período persistem entre abas e no histórico do navegador.
- [ ] Consultas históricas respeitam resolução, limite de pontos, cobertura e gaps retornados pelo backend.
- [ ] Streaming live funciona sem reconsultar histórico em cada evento.
- [ ] Logout e sessão expirada limpam dados protegidos do cliente.

### UX e robustez

- [ ] Estados loading, empty, partial, stale, offline e error são distintos e testados.
- [ ] Erros de um painel não impedem que os demais renderizem.
- [ ] SSE reconecta corretamente sem conexões duplicadas ou vazamentos.
- [ ] KPIs não sofrem layout shift significativo durante atualização.
- [ ] Página não possui rolagem horizontal involuntária nos breakpoints definidos.
- [ ] Navegação por teclado, foco e contraste são auditados; sem bloqueadores WCAG 2.2 AA conhecidos.
- [ ] Aparência segue Design System v1: Industrial Dark Hybrid, cantos retos e bordas 2px intencionais.

### Engenharia e entrega

- [ ] Tipos/cliente da API gerados ou validados contra o OpenAPI real da v0.2.
- [ ] Sem novos endpoints/campos fictícios; discrepâncias de contrato documentadas.
- [ ] Testes de unidade/integração frontend e E2E principais passam.
- [ ] Build de produção funciona e não exige serviço externo obrigatório em runtime.
- [ ] Avaliação de bundle/performance e teste de sessão longa documentados (registrar medições reais, não inventar metas atingidas).
- [ ] `IMPLEMENTATION_SUMMARY_V0.3.md` e `MANUAL_TESTS_V0.3.md` entregues com evidências.
- [ ] Sem tag `v0.3.0` antes de auditoria e validação manual.

## 15. Sequência de entrega

1. Auditar OpenAPI/backend v0.2 e definir contrato efetivo consumível.
2. Implementar tokens, shell, auth e componentes fundacionais, com exemplos de estados.
3. Implementar cliente REST, SSE central e modelo de freshness.
4. Construir Overview e Host por subsistema; Settings mínimo.
5. Polir estados, acessibilidade, responsividade e gráficos.
6. E2E integrado na VM Ubuntu Server 26.04.1 LTS; documentar evidências e limitações.

**Não transformar a v0.3 em gerenciamento de infraestrutura.** Esta entrega é a fundação visual e experiencial da plataforma.
