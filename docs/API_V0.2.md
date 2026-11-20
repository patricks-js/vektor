# API e operação do Vektor v0.2

O `vektord` serve HTTP no mesmo processo que mantém a coleta e o histórico SQLite. Execute-o com um usuário sem privilégios e dono do diretório de dados. A plataforma oficial é Ubuntu Server 26.04.1 LTS.

## Instalação e bootstrap

~~~bash
go build -o ./bin/vektord ./cmd/vektord
umask 077
mkdir -p ./data
./bin/vektord bootstrap-admin --data-dir ./data
./bin/vektord --data-dir ./data
~~~

O primeiro comando administrativo solicita a senha e a confirmação sem eco no terminal. A conta se chama `admin` e exige senha UTF-8 de 12 a 1024 bytes. Para automação, o comando também aceita uma única linha pela entrada padrão, por exemplo redirecionada de um arquivo fornecido por um gestor de segredos. A conta administrativa só pode ser criada uma vez por banco. Antes do bootstrap, o daemon já coleta métricas e o login retorna `401`.

Para revogar todas as sessões, execute `./bin/vektord revoke-sessions --data-dir ./data` como o usuário do daemon. Os cookies existentes passam a receber `401` imediatamente. Um stream aberto já autenticado percebe a revogação pela CLI na próxima verificação de heartbeat, em até 15 segundos em condições normais. O logout pelo HTTP sinaliza os streams imediatamente após gravar a revogação. Uma escrita já em andamento tem prazo de até 3 segundos.

## Configuração

| Flag | Variável de ambiente | Padrão |
| --- | --- | --- |
| `--data-dir` | `VEKTOR_DATA_DIR` | `./data` |
| `--http-addr` | `VEKTOR_HTTP_ADDR` | `127.0.0.1:8080` |
| `--public-origin` | `VEKTOR_PUBLIC_ORIGIN` | Origem inferida do esquema e Host da conexão |
| `--tls-cert` | `VEKTOR_TLS_CERT` | Vazio |
| `--tls-key` | `VEKTOR_TLS_KEY` | Vazio |
| `--session-ttl` | `VEKTOR_SESSION_TTL` | `24h`, configurável de `1m` a `168h` |
| `--max-live-clients` | `VEKTOR_MAX_LIVE_CLIENTS` | `16`, configurável de `1` a `64` |
| `--persist-interval` | `VEKTOR_PERSIST_INTERVAL` | `30s` |
| `--live-interval` | `VEKTOR_LIVE_INTERVAL` | `2s` |
| `--discovery-interval` | `VEKTOR_DISCOVERY_INTERVAL` | `60s` |
| `--log-level` | `VEKTOR_LOG_LEVEL` | `info` |

Flags substituem valores válidos das variáveis de ambiente correspondentes. `--http-addr` aceita `host:porta`, por exemplo `127.0.0.1:9090` ou `[::1]:8080`. A origem pública, quando configurada, deve incluir esquema e autoridade sem barra final, caminho, query ou fragmento. Mudanças de TTL afetam sessões criadas a partir da alteração.

O servidor limita o cabeçalho a 16 KiB, a query string a 2 KiB e o JSON de login a 8 KiB. Mantém até 128 conexões TCP, oito requisições comuns e o número configurado de streams. O prazo para ler cabeçalhos é 3 s, para ler a requisição é 5 s e para manter conexões ociosas é 30 s. Requisições comuns recebem contexto de 8 s e prazo de escrita de 10 s. Cada escrita SSE tem prazo de 3 s. Em `SIGINT` e `SIGTERM`, o daemon cancela as operações, fecha os streams, aguarda até 5 s pelo HTTP e faz checkpoint SQLite passivo.

## Acesso remoto

- Para TLS direto, forneça certificados PEM e chave legível pelo usuário do serviço. Por exemplo, `vektord --http-addr 192.0.2.10:8443 --tls-cert /etc/vektor/fullchain.pem --tls-key /etc/vektor/privkey.pem`, usando o endereço real da interface. Publique no firewall apenas para a rede administrativa e use um certificado confiável para o nome que os clientes acessam.
- Para terminação TLS em um proxy local, mantenha `--http-addr 127.0.0.1:8080` e defina `--public-origin https://monitor.example` com sua origem real. O proxy encaminha `/api/v1/` ao Vektor. Em uma compilação com a interface web, encaminhe também as rotas da interface e `/assets/` na mesma origem. O proxy preserva `Origin` e cookies, habilita HSTS na entrada HTTPS e desativa buffering de SSE. Configure o timeout de leitura para mais de 15 s, por exemplo 60 s entre eventos.
- O Vektor ignora cabeçalhos `Forwarded` e `X-Forwarded-*` para origem, TLS e rate limiting. Em um proxy, os clientes compartilham o endereço de origem visto pelo daemon. Se houver vários operadores atrás do mesmo proxy, adicione limitação por cliente também no proxy. Configure os logs do proxy para omitir cabeçalhos Cookie, Set-Cookie e X-CSRF-Token e corpos de login.
- Use HTTP em loopback para desenvolvimento ou um túnel SSH, por exemplo `ssh -L 8080:127.0.0.1:8080 operador@servidor`. O cookie usa `HttpOnly`, `SameSite=Strict`, `Path=/api/v1`, `Expires` e `Max-Age`. O Vektor acrescenta `Secure` em TLS direto ou quando uma origem pública HTTPS está configurada.

O login exige um documento JSON e o logout exige o cabeçalho `X-CSRF-Token` emitido pela própria sessão. Se a requisição mutável inclui `Origin`, esse cabeçalho precisa corresponder à origem esperada. O serviço rejeita mutações marcadas como cross-site e não concede acesso CORS. Com bind em loopback e origem pública inferida, também rejeita Host que não seja loopback.

O limitador em memória aceita até cinco tentativas de login por IP e vinte no total por janela de um minuto, identifica o endereço da conexão e admite uma derivação Argon2id simultânea. Respostas `429` incluem `Retry-After`. Até 256 sessões podem estar ativas. O Vektor elimina sessões vencidas ao criar uma nova. Logout remove apenas a sessão atual.

## REST e streaming

O [OpenAPI](openapi-v1.yaml) define os tipos e códigos HTTP. A tabela resume as rotas.

| Método e caminho | Resultado |
| --- | --- |
| `GET /api/v1/host` | ID do host, inventário da descoberta mais recente e uptime Linux |
| `GET /api/v1/resources` | Página ordenada por ID com filtros `kind` e `state=active\|inactive\|all` |
| `GET /api/v1/resources/{id}` | Recurso ativo ou inativo identificado pelo ID persistente |
| `GET /api/v1/metrics` | Um recurso, uma métrica do catálogo e uma resolução efetiva |
| `GET /api/v1/health` | Coleta e escrita recentes, contadores de erros, status do relógio e SQLite |
| `GET /api/v1/stream/live` | Snapshots efêmeros e comentários heartbeat |
| `POST /api/v1/auth/login` | Cookie, `expires_at` e `csrf_token` |
| `POST /api/v1/auth/logout` | Revogação e expiração do cookie |
| `GET /api/v1/auth/me` | `username`, `expires_at` e `csrf_token` da sessão |

A listagem usa `limit` de 1 a 100, com 50 por padrão. Use `next_cursor` como `cursor` na página seguinte, mantendo os filtros. O campo `metadata_json` contém a representação JSON do registry como string.

Em métricas, `from` e `to` aceitam milissegundos Unix a partir do epoch ou datas RFC 3339. A janela máxima é 90 dias. `step=auto` usa o algoritmo do QueryService para escolher entre os níveis 30 s, 5 min e 1 h dentro do orçamento de pontos. Pode-se fixar um nível com `step=30`, `step=300`, `step=3600` ou os aliases registrados no OpenAPI. `max_points` aceita de 1 a 2000 e tem padrão 600. O início é alinhado ao bucket que contém `from` e cada início retornado precede `to`. Pontos faltantes ficam ausentes e uma medição igual a zero permanece na série. Sem dados, o modo automático retorna resolução de 30 s e uma lista vazia. Cada ponto inclui contagem observada, contagem esperada e cobertura calculada pela cadência efetiva de persistência e da categoria de coletor.

Os erros gerados pela API incluem `code` e `message`. Uma janela acima de 90 dias ou sem resolução que caiba no limite retorna `422`. Um diagnóstico de banco degradado pode incluir `diagnostics` junto ao erro `503`, quando a sessão ainda puder ser consultada. Limites do transporte, como cabeçalhos excessivos, podem ser tratados pelo próprio `net/http`.

SSE inicia no primeiro assinante e compartilha o scheduler com os demais. Cada evento `snapshot` traz `collected_at` e pontos válidos com ID, métrica, timestamp e valor. A API envia até 8.192 pontos por evento e marca `truncated` se houver mais. `warning_count` contabiliza avisos do snapshot. O canal de cada consumidor comporta quatro snapshots e descarta novas entregas quando cheio. O comentário `: heartbeat` chega a cada 15 s e reconexões recebem snapshots novos. O streaming lê a sessão no SQLite e publica exclusivamente em memória.

## Exemplos com curl

Execute em Bash no host ou túnel. Mantenha o cookie e o token CSRF em um diretório temporário privado.

~~~bash
base=http://127.0.0.1:8080
umask 077
api_tmp=$(mktemp -d)
jar="$api_tmp/cookies"
read -rsp 'Senha de admin: ' vektor_password
printf '\n'
printf '%s' "$vektor_password" |
  jq -Rs '{username:"admin", password:.}' |
  curl -fsS -c "$jar" -H 'Content-Type: application/json' --data-binary @- \
    "$base/api/v1/auth/login" > "$api_tmp/session.json"
unset vektor_password

curl -fsS -b "$jar" "$base/api/v1/host"
curl -fsS -b "$jar" "$base/api/v1/resources?kind=nic&state=all"
curl -fsS -b "$jar" "$base/api/v1/resources/host:local"
curl -fsS -b "$jar" "$base/api/v1/health"
curl -fsS -b "$jar" -G "$base/api/v1/metrics" \
  --data-urlencode 'resource_id=host:local' \
  --data-urlencode 'metric=load.1m' \
  --data-urlencode "from=$(date -u -d '15 minutes ago' +%FT%TZ)" \
  --data-urlencode "to=$(date -u +%FT%TZ)" \
  --data-urlencode 'step=auto'
curl -fsS -N -b "$jar" "$base/api/v1/stream/live"
~~~

Encerre o último `curl` com Ctrl+C. Consulte e revogue a sessão usando o mesmo cookie.

~~~bash
curl -fsS -b "$jar" "$base/api/v1/auth/me" > "$api_tmp/session.json"
jq -er '"X-CSRF-Token: " + .csrf_token' "$api_tmp/session.json" > "$api_tmp/csrf.header"
curl -fsS -X POST -b "$jar" -c "$jar" -H @"$api_tmp/csrf.header" \
  "$base/api/v1/auth/logout"
rm -- "$jar" "$api_tmp/session.json" "$api_tmp/csrf.header"
rmdir -- "$api_tmp"
~~~

## Atualização da v0.1 para a v0.2

1. Em uma conta sem privilégios, compile o novo binário e pare o `vektord` v0.1. Guarde o binário anterior para rollback.
2. Faça uma cópia consistente enquanto o daemon estiver parado. Com o diretório real em `state`, execute os comandos abaixo. O backup inclui dados confirmados no WAL. Restrinja o acesso à cópia.
3. Execute `./bin/vektord bootstrap-admin --data-dir "$state"` como o usuário proprietário do diretório. A migração 003 cria as tabelas de autenticação e preserva as tabelas históricas e os IDs. O daemon também aplica a migração quando inicia. O arquivo SQLite e os sidecars são restritos ao proprietário.
4. Inicie a v0.2 com o mesmo diretório e verifique login, `/health` e uma faixa histórica anterior à atualização. Em caso de rollback, pare a v0.2, preserve uma cópia do estado atual e restaure o snapshot anterior em um diretório separado para a v0.1. O snapshot contém o histórico até a data da cópia.

~~~bash
umask 077
sqlite3 -readonly "$state/vektor.db" ".backup '$state/vektor-pre-v02.db'"
sqlite3 -readonly "$state/vektor-pre-v02.db" 'PRAGMA quick_check;'
~~~
