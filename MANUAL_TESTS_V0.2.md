# Roteiro manual da v0.2 no Ubuntu Server 26.04.1 LTS

Execute em uma VM de teste com NTP sincronizado, usuário sem privilégios, Go, Bash, curl, jq, sqlite3, ripgrep e OpenSSL. Rode a partir da raiz do repositório. O roteiro usa somente diretórios temporários e portas de loopback. Registre códigos HTTP, saídas sanitizadas, versão do sistema e tempos observados.

## 1. Ambiente, coleta sem clientes e bootstrap

~~~bash
cat /etc/os-release
uname -r
id -u
timedatectl show -p NTPSynchronized --value
go version
go build -o ./bin/vektord ./cmd/vektord

umask 077
work=$(mktemp -d)
state="$work/state"
jar="$work/cookies"
base=http://127.0.0.1:8080
mkdir -m 700 "$state"

./bin/vektord --data-dir "$state" --persist-interval 10s > "$work/daemon.log" 2>&1 &
daemon=$!
sleep 2
ss -ltn | rg '127\.0\.0\.1:8080'
curl -sS -o /dev/null -w '%{http_code}\n' "$base/api/v1/host"
sleep 25
sqlite3 -readonly "$state/vektor.db" \
  "SELECT COUNT(*) FROM metric_buckets WHERE resource_id='host:local' AND resolution_s=30;"

./bin/vektord bootstrap-admin --data-dir "$state"
./bin/vektord bootstrap-admin --data-dir "$state"
stat -c '%a %n' "$state/vektor.db"
~~~

Espere UID diferente de 0, listener em `127.0.0.1:8080`, `401` sem cookie e pelo menos uma amostra escrita antes do bootstrap. A primeira execução administrativa exibe a confirmação da criação sem ecoar a senha. A segunda termina com código diferente de zero após receber uma senha válida. A permissão do banco deve ser `600`.

## 2. Credenciais, sessão e REST

~~~bash
login() {
  local pass target
  target="${1:-$jar}"
  read -rsp 'Senha de admin: ' pass
  printf '\n' >&2
  printf '%s' "$pass" |
    jq -Rs '{username:"admin", password:.}' |
    curl -fsS -c "$target" -H 'Content-Type: application/json' --data-binary @- \
      "$base/api/v1/auth/login" > "$work/session.json"
}

curl -sS -o /dev/null -w '%{http_code}\n' -H 'Content-Type: application/json' \
  --data '{"username":"admin","password":"senha de teste incorreta"}' "$base/api/v1/auth/login"
login
for path in host resources metrics stream/live health auth/me; do
  curl -sS -o /dev/null -w "$path %{http_code}\n" "$base/api/v1/$path"
done

curl -fsS -b "$jar" "$base/api/v1/host" | jq -e '.inventory.hostname and (.uptime_seconds >= 0)'
curl -fsS -b "$jar" "$base/api/v1/resources?state=all&limit=1" | jq .
curl -fsS -b "$jar" "$base/api/v1/resources?kind=nic&state=inactive" | jq .
curl -fsS -b "$jar" "$base/api/v1/resources/host:local" | jq -e '.id == "host:local"'
curl -sS -b "$jar" -w '\n%{http_code}\n' "$base/api/v1/resources/ausente:1"
curl -fsS -b "$jar" "$base/api/v1/health" | jq '{status, db_ping_ok, last_collect_at, last_write_at}'

range_from=$(date -u -d '10 minutes ago' +%s%3N)
range_to=$(date -u +%s%3N)
series="$base/api/v1/metrics?resource_id=host:local&metric=load.1m&from=$range_from&to=$range_to&step=auto"
curl -fsS -b "$jar" "$series" > "$work/history-before.json"
jq -e '(.points | length) > 0 and all(.points[]; .coverage >= 0 and .coverage <= 1)' "$work/history-before.json"
curl -sS -b "$jar" -w '\n%{http_code}\n' "$base/api/v1/metrics?resource_id=host:local&metric=inexistente&from=0&to=1"
curl -sS -b "$jar" -w '\n%{http_code}\n' "$base/api/v1/metrics?resource_id=host:local&metric=load.1m&from=abc&to=1"
curl -sS -b "$jar" -w '\n%{http_code}\n' "$base/api/v1/metrics?resource_id=host:local&metric=load.1m&from=0&to=7862400000"
curl -sS -b "$jar" -w '\n%{http_code}\n' "$series&max_points=2001"
curl -fsS -b "$jar" "$base/api/v1/metrics?resource_id=host:local&metric=load.1m&from=0&to=1&step=auto" |
  jq -e '.resolution_s == 30 and .points == []'
~~~

Espere `401` para a senha errada e para todas as consultas privadas sem cookie. Com o cookie, espere `200` em host, recursos, detalhes, saúde e métricas. O recurso inexistente retorna `404`, parâmetros inválidos retornam `400`, e 91 dias retornam `422`. Uma consulta válida sem dados retorna uma lista vazia. Se a VM tiver histórico suficiente com pelo menos dois buckets raw, troque `step=auto` por `step=30&max_points=1` e espere `422 point_limit`.

Recursos inativos só aparecem se já tiverem sido descobertos e removidos. Para exercitar a remoção em VM isolada com console, adicione e remova uma NIC secundária pela plataforma de virtualização, aguardando um intervalo de descoberta entre cada operação. Guarde seu ID em `state=active`, verifique o mesmo ID com `state=inactive` após a remoção e volte a consultar após reconectar o mesmo dispositivo virtual.

## 3. SSE, escrita simultânea e reinício

~~~bash
curl -fsS -N -b "$jar" "$base/api/v1/stream/live" > "$work/live1.txt" &
stream1=$!
curl -fsS -N -b "$jar" "$base/api/v1/stream/live" > "$work/live2.txt" &
stream2=$!
sleep 2
curl -fsS -b "$jar" "$base/api/v1/health" | jq -e '.active_live_subscribers == 2'
before_write=$(curl -fsS -b "$jar" "$base/api/v1/health" | jq -r .last_write_at)
sleep 16
after_write=$(curl -fsS -b "$jar" "$base/api/v1/health" | jq -r .last_write_at)
test "$before_write" != "$after_write"
for log in "$work/live1.txt" "$work/live2.txt"; do
  rg -m 1 '^event: snapshot$' "$log"
  rg -m 1 '^: heartbeat$' "$log"
  sed -n 's/^data: //p' "$log" | jq -es 'length > 0 and all(.[]; has("points"))'
done
kill "$stream1" "$stream2"
wait "$stream1" "$stream2" 2>/dev/null || true
sleep 2
curl -fsS -b "$jar" "$base/api/v1/health" | jq -e '.active_live_subscribers == 0'

curl -fsS -N -b "$jar" "$base/api/v1/stream/live" > "$work/reconnected.txt" &
stream3=$!
sleep 2
curl -fsS -b "$jar" "$base/api/v1/health" | jq -e '.active_live_subscribers == 1'
kill -TERM "$daemon"
wait "$daemon"
wait "$stream3"
ss -ltn | rg '127\.0\.0\.1:8080'

./bin/vektord --data-dir "$state" --persist-interval 10s >> "$work/daemon.log" 2>&1 &
daemon=$!
sleep 2
curl -fsS -b "$jar" "$base/api/v1/auth/me" | jq -e '.username == "admin"'
curl -fsS -b "$jar" "$series" > "$work/history-after.json"
jq -e -s '.[0].points as $previous | .[1].points as $current |
  all($previous[]; .bucket_start_ms as $at | any($current[]; .bucket_start_ms == $at))' \
  "$work/history-before.json" "$work/history-after.json"
sqlite3 -readonly "$state/vektor.db" 'PRAGMA quick_check;'
~~~

Espere snapshots e heartbeats em ambas as conexões e `last_write_at` avançando enquanto o SSE está ativo. Ao desconectar o último cliente, espere zero assinantes. O `SIGTERM` encerra a conexão por EOF e libera a porta. Após reiniciar, o cookie anterior funciona, os buckets anteriores seguem disponíveis e o SQLite responde `ok`.

O teste automatizado abaixo satura um socket real de modo controlado e verifica a manutenção do cliente rápido, o prazo de escrita e ciclos repetidos. Usa `PRAGMA data_version` para verificar que o SSE sozinho não gera commits no SQLite.

~~~bash
go test -race -count=1 -v ./tests/integration \
  -run '^TestHTTPSSE(NonReadingClient|OnDemandSharedCollectionAndNoSQLiteWrites|PersistenceAndLogout)$'
~~~

## 4. Origem, CSRF, revogação e rate limiting

~~~bash
logout_url="$base/api/v1/auth/logout"
curl -sS -b "$jar" -X POST -w '\n%{http_code}\n' "$logout_url"
curl -fsS -b "$jar" "$base/api/v1/auth/me" > "$work/session.json"
jq -er '"X-CSRF-Token: " + .csrf_token' "$work/session.json" > "$work/csrf.header"
curl -sS -b "$jar" -H @"$work/csrf.header" -H 'Origin: https://foreign.example' \
  -X POST -w '\n%{http_code}\n' "$logout_url"
curl -fsS -b "$jar" -H @"$work/csrf.header" -X POST "$logout_url"
curl -sS -b "$jar" -w '\n%{http_code}\n' "$base/api/v1/auth/me"

login "$work/revoke.cookies"
./bin/vektord revoke-sessions --data-dir "$state"
curl -sS -b "$work/revoke.cookies" -w '\n%{http_code}\n' "$base/api/v1/auth/me"
sleep 61
for i in {1..6}; do
  curl -sS -o /dev/null -w '%{http_code}\n' -H 'Content-Type: application/json' \
    --data '{"username":"admin","password":"senha de teste incorreta"}' "$base/api/v1/auth/login"
done

awk 'NF >= 7 && $6 == "vektor_session" {print $7}' "$jar" "$work/revoke.cookies" > "$work/session-values"
test -s "$work/session-values"
if rg -q -F -f "$work/session-values" "$work/daemon.log"; then
  printf '%s\n' 'FALHA. O log contém um valor de cookie.'
fi
kill -INT "$daemon"
wait "$daemon"
~~~

Espere `403 csrf_denied` sem CSRF e `403 origin_denied` na origem externa. A revogação por logout e por CLI devem tornar os cookies guardados inválidos com `401`. O limite deve produzir cinco respostas `401` e uma `429`, com `Retry-After`. O log do daemon não deve conter os valores dos cookies.

## 5. TLS direto e expiração

~~~bash
openssl req -x509 -nodes -newkey rsa:2048 -days 1 -subj '/CN=localhost' \
  -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1' \
  -keyout "$work/key.pem" -out "$work/cert.pem"
./bin/vektord --data-dir "$state" --http-addr 127.0.0.1:9443 --session-ttl 1m \
  --tls-cert "$work/cert.pem" --tls-key "$work/key.pem" >> "$work/daemon.log" 2>&1 &
daemon=$!
sleep 2
base=https://127.0.0.1:9443
read -rsp 'Senha de admin: ' pass
printf '\n'
printf '%s' "$pass" |
  jq -Rs '{username:"admin", password:.}' |
  curl -fsS --cacert "$work/cert.pem" -D "$work/tls.headers" -c "$work/tls.cookies" \
    -H 'Content-Type: application/json' --data-binary @- "$base/api/v1/auth/login" > "$work/session.json"
unset pass
rg -qi '^set-cookie:.*Path=/api/v1;.*Max-Age=60;.*HttpOnly;.*Secure;.*SameSite=Strict' "$work/tls.headers"
rg -qi '^strict-transport-security:' "$work/tls.headers"
curl -fsS --cacert "$work/cert.pem" -b "$work/tls.cookies" "$base/api/v1/auth/me" | jq -e '.username == "admin"'
sleep 61
curl -sS --cacert "$work/cert.pem" -b "$work/tls.cookies" -o /dev/null -w '%{http_code}\n' "$base/api/v1/auth/me"
kill -TERM "$daemon"
wait "$daemon"
~~~

Espere os atributos Secure, HttpOnly, SameSite Strict, Path e Max-Age, além de HSTS. A sessão retorna `200` antes do vencimento e `401` depois. O teste automatizado reapresenta o cookie mesmo após o vencimento para verificar a validação no servidor.

## 6. Atualização de uma cópia v0.1 e registro

Use uma cópia descartável de um banco real da v0.1 e siga o [procedimento de atualização](docs/API_V0.2.md#atualização-da-v01-para-a-v02). Registre contagem de buckets, watermarks e IDs antes e depois com o daemon parado. A migração e o bootstrap preservam essas leituras. Compare com uma consulta HTTP a um intervalo antigo e guarde o resultado de `PRAGMA quick_check`.

~~~bash
sqlite3 -readonly "$state/vektor.db" \
  'SELECT version,name FROM schema_migrations ORDER BY version;
   SELECT resolution_s,COUNT(*) FROM metric_buckets GROUP BY resolution_s;
   SELECT resolution_s,closed_through_ms FROM rollup_state ORDER BY resolution_s;
   SELECT id,kind FROM resources ORDER BY id;'
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
~~~

Registre as diferenças entre esperado e observado, incluindo o estado de sincronização do relógio. Para arquivar resultados use saídas sanitizadas. O diretório temporário contém senha derivada, arquivos de sessão e a chave TLS de teste e deve ser removido ao concluir.
