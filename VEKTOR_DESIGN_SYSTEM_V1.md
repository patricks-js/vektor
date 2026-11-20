# VEKTOR — Design System v1.0

**Direção aprovada:** Industrial Dark Hybrid — base *Industrial Refined*, acentos *Industrial Brutalist*.
**Aplicação:** Vektor Web v0.3 e futuras interfaces de gerenciamento.
**Status:** Especificação de design (não equivale a implementação).

## 1. Visão e princípios

Vektor é uma ferramenta de infraestrutura, não um template genérico de analytics. Sua identidade deve parecer um instrumento de engenharia preciso, confiável e contemporâneo. O visual é escuro, plano, organizado em grade, com cantos predominantemente retos. Bordas são permitidas — inclusive 2px — quando reforçam hierarquia, foco, seleção ou ações importantes.

1. **Geometry first:** retângulos, alinhamentos rigorosos, cantos 0–4px. Não usar pills indiscriminadamente.
2. **Borders with purpose:** 1px para estrutura, 2px para foco/seleção e alguns painéis destacados. Evitar aplicar 2px em tudo.
3. **Data over decoration:** alta legibilidade, precisão numérica e gráficos comparáveis; sem gradientes excessivos ou efeitos cenográficos.
4. **Progressive disclosure:** informação essencial primeiro; análise aprofundada por subsistema, filtros e detalhes expansíveis.
5. **Operational truth:** zero, ausência, atraso, erro e desconexão são estados diferentes.
6. **Accessible by default:** contraste, foco, teclado, semântica, responsividade, redução de movimento.

## 2. Tokens fundacionais

### 2.1 Cores — modo escuro

| Token | Hex | Uso |
|---|---|---|
| `--vk-bg` | `#101317` | Canvas principal |
| `--vk-surface-1` | `#191F24` | Painéis e sidebar |
| `--vk-surface-2` | `#222A30` | Controles, elevação sutil |
| `--vk-surface-3` | `#2C353C` | Hover e seleção neutra |
| `--vk-border-subtle` | `#343D44` | Bordas e divisórias padrão |
| `--vk-border-strong` | `#53616B` | Superfícies enfatizadas |
| `--vk-text-primary` | `#F1F3F4` | Títulos e valores |
| `--vk-text-secondary` | `#B2BDC5` | Texto de apoio |
| `--vk-text-muted` | `#8996A1` | Metadados; validar contraste para conteúdo pequeno |
| `--vk-accent` | `#B5F277` | Marca, seleção, CTA e ênfase restrita |
| `--vk-accent-ink` | `#101317` | Texto sobre cor de marca |
| `--vk-success` | `#69D8A0` | Estado de sucesso |
| `--vk-warning` | `#F2C66D` | Atenção, stale, reconnecting |
| `--vk-critical` | `#F17D83` | Falha ou condição crítica |
| `--vk-info` | `#7DB9F2` | Mensagens informativas |
| `--vk-unknown` | `#8995A0` | Estado indeterminado |

**Regras:** cor da marca ≠ saúde operacional; não usar cor como único canal informacional; validar WCAG AA na combinação final, em particular texto muted sobre superfícies, estados desabilitados e gráficos. Sempre incluir rótulo, ícone ou padrão quando uma cor tiver significado. Acessibilidade vem antes da fidelidade cega aos hex.

### 2.2 Geometria, bordas e sombras

- `--vk-radius-0: 0px` — **padrão** de cards, botões, tabelas, sidebar e chart panels.
- `--vk-radius-1: 2px` — campos e pequenos controles quando útil.
- `--vk-radius-2: 4px` — exceções, como popovers e dialogs. Sem grandes arredondamentos ou pills.
- `--vk-stroke-1: 1px` — bordas estruturais.
- `--vk-stroke-2: 2px` — foco visível, item selecionado, componente-chave com propósito.
- Sombras quase ausentes; use contraste de superfície, alinhamento e traço.
- Nunca usar borda 2px em todos os componentes de uma mesma grade; sua força deve permanecer rara.
- Foco de teclado: outline visível >=2px, com offset que não seja cortado.

### 2.3 Tipografia

- **Geist Sans** (ou alternativa disponível/fallback de sistema) para UI.
- **Geist Mono** (ou fallback monoespaçado) para IP, porta, nomes técnicos, valores tabulares e IDs.
- `font-variant-numeric: tabular-nums` em métricas que atualizam; evitar layout shift.

| Estilo | Especificação inicial |
|---|---|
| Page title | 28px / 34px / 600 |
| Section title | 18px / 26px / 600 |
| Metric hero | 36px / 40px / 600, tabular |
| Body | 14px / 21px / 400 |
| Secondary | 13px / 19px / 400 |
| Technical label | 12px / 16px / 500, letter spacing discreto |

Não usar texto técnico em CAPS extensivamente; labels podem usar maiúsculas com parcimônia. Preferir formato de unidades estável: `32.4%`, `4.2 GiB`, `18.7 MiB/s`, `54 °C`.

### 2.4 Espaçamento e dimensões

- Escala de 4px: 4, 8, 12, 16, 24, 32, 40 e 48px.
- Altura de controles interativos em desktop: alvo 40px; área de toque mobile ~44px quando possível.
- Gaps de cards: 12–16px; padding: 16–24px; seções principais: 24–32px.
- Sidebar desktop: ~232px expandida; ~64px recolhida; header da página com altura visual consistente.
- Layout principal: largura fluida com limite sensato para leitura; gráficos podem ocupar toda a largura disponível.
- Tabelas: linhas ~44–52px (modo Comfortable); modo Compact pode ser posterior.

### 2.5 Motion

- Transições funcionais rápidas (~120–180ms), sem movimentos grandes na coleta live.
- `prefers-reduced-motion` deve desabilitar animações dispensáveis.
- Nunca animar números com efeito de contagem; mudanças de séries não devem reorganizar a página.

## 3. Gramática visual Hybrid

**Refined (padrão):** superfícies neutras, divisórias 1px, tipografia limpa, alinhamento em grade e espaços generosos.

**Brutalist (ênfase pontual):** borda 2px em painel central, indicadores de seleção, foco, ações primárias importantes e cabeçalhos estruturantes. Pode haver labels técnicas e marcadores geométricos, sem exagero visual.

**Antipadrões:** cards com radius 12–24px; botões pill; glassmorphism; gradientes ornamentais; sombras profundas; cores neon em todos os gráficos; excesso de molduras espessas; texto ilegível por ser “minimalista”.

## 4. Componentes e variantes

| Componente | Anatomia mínima | Estados/variantes |
|---|---|---|
| `AppShell` | sidebar, header, conteúdo, status de conexão | expanded/collapsed/mobile |
| `NavItem` | ícone, label, indicador ativo | default/hover/active/focus |
| `Button` | label, ícone opcional | primary/outline/quiet/danger; loading/disabled |
| `TextField` | label, input, hint, erro | idle/focus/invalid/disabled |
| `StatusIndicator` | símbolo + texto | live/reconnecting/stale/offline; *não* confundir com saúde |
| `MetricCard` | label, valor, unidade, contexto, estado | healthy/partial/unavailable/loading |
| `ChartPanel` | título, legenda, período, gráfico, metadados | loading/empty/partial/error/live |
| `TimeRangeSelector` | opções 15m/1h/6h/24h/7d, custom se backend permitir | active/focus/disabled |
| `ResourceTable` | ID/nome/estado/valores, ordenação | loading/empty/filtered/error |
| `Tabs` | subsistemas do Host | selected/hover/focus |
| `Tooltip` | timestamp, série, valor e unidade | teclado/mouse |
| `EmptyState` | razão do vazio e ação relevante | no-data/no-results/not-supported |
| `ErrorState` | código acessível, explicação, retry | network/auth/query/server |
| `Dialog` | título, conteúdo, ação, cancelar | focus trap/escape/return focus |
| `Toast` | feedback não bloqueante | success/info/warning/error |

### Regras de comportamento

- Componentes acionáveis devem exibir hover, foco visível, loading e disabled quando cabíveis.
- Não usar tooltips como única forma de apresentar dados essenciais.
- Não truncar IPs e identificadores sem caminho para ver/copiá-los.
- Tabelas devem permitir navegação e ordenação por teclado.
- Acessibilidade mínima: WCAG 2.2 AA como objetivo de projeto; texto normal ≥4.5:1, texto grande e componentes não textuais ≥3:1, conforme aplicável.

## 5. Visualização de dados

- Métricas devem manter convenção de cor entre telas; reservar verde-lima para acento de marca/seleção, não para toda série.
- Rótulos e unidades explícitos. Não juntar métricas de unidades diferentes no mesmo eixo sem justificativa.
- Séries temporais: lacuna = segmento ausente; **jamais** interpolar silenciosamente. Zero = ponto mensurado igual a zero. Sem dado = estado distinto.
- Tooltip sincronizado por timestamp entre gráficos comparáveis.
- Filtro temporal compartilhado por investigação; refletir período e aba na URL, quando aplicável.
- Exibir `resolution` e `coverage` retornados pela API; se não houver correspondência semântica, indicar indisponibilidade em vez de inventar.
- Live: valores ao vivo via SSE; histórico via REST no ritmo apropriado. Não assumir que SSE aumenta frequência de persistência.
- Registrar time zone usada no eixo/tooltip e oferecer timestamps absolutos no detalhe.
- Usar unidades coerentes para bytes, taxas e porcentagens; distinguir capacidade de filesystem de I/O por dispositivo.

## 6. Responsividade e densidade

| Viewport | Shell | Conteúdo |
|---|---|---|
| ≥1440px | sidebar expandida | 4 KPIs em linha, gráficos em duas colunas quando útil |
| 1024–1439px | sidebar expandida ou recolhível | cards e gráficos adaptativos |
| 768–1023px | sidebar recolhida | KPIs em 2 colunas, gráficos em largura total |
| <768px | menu acessível | KPIs empilhados/2 colunas se couber; tabelas com adaptação explícita |

Sem rolagem horizontal **da página** nos tamanhos de referência. Scroll horizontal dentro de tabelas muito densas é aceitável se indicado, acessível e restrito ao componente. Priorizar desktop sem negligenciar mobile.

## 7. Implementação e governança

- Design tokens como CSS custom properties; Tailwind pode consumi-los, sem espalhar hex pelo código.
- Biblioteca Vektor própria sobre primitives acessíveis; não importar um tema pronto.
- Documentar variantes e estados em catálogo de componentes (Storybook ou equivalente, se o custo se justificar).
- Testes visuais em 3 larguras mínimas e estados `loading`, `empty`, `error`, `partial` e `live`.
- Qualquer novo componente precisa ter propósito, tokens, comportamento por teclado, estados e exemplo de uso.
- Não criar design tokens ou APIs abstratas sem caso de uso real.

## 8. Critérios de aceite do Design System

- [ ] Tokens semânticos centralizados; nenhum componente principal usa cores hardcoded fora de tokens.
- [ ] Radius 0px como padrão; nenhum botão pill ou card excessivamente arredondado.
- [ ] Bordas 2px presentes apenas como hierarquia/seleção/foco, não indiscriminadamente.
- [ ] Contraste, foco visível e navegação por teclado auditados.
- [ ] Estados de loading/empty/error/partial/disabled implementados nos componentes aplicáveis.
- [ ] Tipografia numérica tabular evita deslocamento de layout em live.
- [ ] Gráficos representam gaps e valores zero de modo diferente.
- [ ] Layout responsivo sem overflow horizontal da página.
- [ ] Ícones, rótulos, espaçamentos e linguagem visual consistentes entre Login, Overview, Host e Settings.
