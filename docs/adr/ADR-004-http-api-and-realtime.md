# ADR-004 — API HTTP e streaming no processo do daemon

**Status** Aceita

**Data** 2026-11-20

## Contexto

A v0.2 expõe o inventário, o histórico e os snapshots do Vektor por HTTP. O SQLite em WAL já possui pools separados para leituras e escritas. O scheduler já fornece snapshots sob demanda pelo LiveHub.

## Decisão

- O daemon usa `net/http` com bind padrão em loopback e suporte a TLS no próprio processo. O mesmo contexto de ciclo de vida cancela HTTP, SSE e scheduler. As rotas comuns têm prazos de contexto e escrita. O SSE usa prazo por frame e heartbeat, permitindo conexões longas.
- REST delega histórico ao QueryService e mantém os IDs do registry. A API valida entradas, limita a janela e pagina recursos por ID. Erros de dependências e diagnósticos HTTP usam mensagens predefinidas.
- Cada conexão SSE assina o LiveHub existente. O hub publica sem esperar pelo cliente. O servidor limita assinantes, usa o buffer existente de quatro snapshots por cliente e encerra escritas lentas por prazo. A carga SSE contém somente pontos válidos e um contador de avisos, com indicação explícita de truncamento acima de 8.192 pontos.
- Health e monitoramento exigem sessão. O proxy TLS é opcional. O operador pode configurar uma origem pública exata sem delegar confiança a cabeçalhos X-Forwarded-*.

## Consequências

HTTP e SSE compartilham o inventário e a coleta com o pipeline da v0.1. Os streams são efêmeros e o cliente pode consultar o REST para dados históricos disponíveis. Operação remota em cleartext continua dependendo de decisão explícita de bind. O guia operacional descreve TLS, segmentação de rede e a configuração de proxy. [Guia operacional](../API_V0.2.md)
