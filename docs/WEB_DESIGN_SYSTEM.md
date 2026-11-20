# Design System na interface

A interface implementa o Industrial Dark Hybrid com StyleX e primitivas semânticas de HTML. A folha global define propriedades CSS estáveis para paleta, geometria, espaçamento e tipografia. O compilador StyleX agrega estilos atômicos, variantes, estados e media queries no CSS estático do build. O reset, o foco global e as fontes locais ficam na mesma entrada CSS. [Especificação v1](../VEKTOR_DESIGN_SYSTEM_V1.md) · [Tokens CSS](../web/src/styles/global.css) · [Integração Vite e StyleX](https://stylexjs.com/docs/learn/installation/vite/)

- Superfícies e divisórias usam `--vk-surface-*` e `--vk-border-*`. `--vk-border-control` mantém a borda dos controles contrastante sobre superfícies escuras. A borda de 2 px aparece no foco, na navegação ativa e nos painéis que identificam o host ou um recurso.
- `--vk-accent` colore a marca, a ação primária e a seleção. Estados usam `--vk-success`, `--vk-warning` ou `--vk-critical` com ícone e texto. As séries usam `--vk-series-*` por subsistema ou direção de tráfego.
- Geist e Geist Mono são servidas localmente. Valores têm números tabulares. IDs, timestamps e informações técnicas podem quebrar linha. [Licença da tipografia](../web/src/styles/geist-license.txt)
- A barra lateral mede 232 px no desktop e 64 px quando recolhida. Tablet usa 64 px e mobile usa um diálogo nativo. Gráficos ficam em uma coluna em tablet e mobile. Tabelas largas têm sua própria região rolável identificada.

## Catálogo de componentes

| Componente | Uso e exemplo | Estados e teclado |
| --- | --- | --- |
| `Button` | `<Button variant="primary" busy={pending}>Entrar</Button>` | `primary`, `outline`, `quiet`, `danger`. Enter ou Espaço aciona. Desabilitado enquanto ocupado. |
| `TextField`, `SelectField` | Login, busca, filtros e preferências | Rótulo associado ao controle. Campos suportam dica, erro, invalidez e ação auxiliar. |
| `Badge` | Saúde, transporte e estados do inventário | Ícone e texto mantêm o significado visível. O transporte anuncia mudanças de estado. |
| `Panel`, `Facts`, `MetricCard` | Identificação do host e KPIs na Overview | Painel padrão ou enfatizado. Leituras exibem fonte, timestamp ou indisponibilidade quando aplicável. |
| `Loading`, `Empty`, `ErrorState` | Carregamento de inventário e falhas por painel | Estado de carregamento anunciado. Erros incluem código HTTP e tentativa manual quando aplicável. |
| `TimeRangeSelector` | Período da Overview e do Host | Botões com `aria-pressed`. Seleção refletida na URL. |
| `ChartPanel` | Histórico de CPU, RAM, I/O, rede e temperatura | Ponteiro ou controle deslizante com setas e Home/End. Dois gráficos no mesmo contexto compartilham o cursor UTC. |
| `ResourceTable` | Núcleos, mounts, discos, NICs e sensores | Ordenação por nome ou estado, busca, paginação local e links de detalhe. |
| `Modal` | Confirmação de logout | Diálogo nativo com foco contido, Escape e retorno ao botão acionador. |

Exemplos executáveis estão nas telas de login, Overview, Host e Settings. Os testes Playwright capturam a interface em 1440, 1024, 768, 390 e 360 px e exercitam estados de carregamento, erro, vazio, parcial e ao vivo. As capturas ficam em `web/test-results/`. [Componentes base](../web/src/components/ui/ui.tsx) · [Testes visuais](../web/e2e/visual.spec.ts) · [Cenários de dados](../web/e2e/dashboard.spec.ts)
