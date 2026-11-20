# Validação manual da interface v0.3 no Ubuntu Server 26.04.1 LTS

Use uma VM de teste com relógio sincronizado e um usuário sem privilégios. Compile o binário na máquina de desenvolvimento, com Bun e Go, e inicie o da arquitetura da VM. Use um perfil de navegador separado para o teste de autenticação.

## 1. Compilar e iniciar

Na raiz do checkout, compile para a arquitetura da VM. O exemplo usa Linux amd64.

~~~bash
bun install --cwd web --frozen-lockfile
bun run --cwd web build
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags webui -o ./bin/vektord ./cmd/vektord
~~~

Transfira `bin/vektord` para a VM, execute os comandos no diretório que contém o binário e mantenha o daemon em primeiro plano.

~~~bash
cat /etc/os-release
uname -m
id -u
timedatectl show -p NTPSynchronized --value
umask 077
vk_test_state=$(mktemp -d)
./vektord bootstrap-admin --data-dir "$vk_test_state"
./vektord --data-dir "$vk_test_state" --http-addr 127.0.0.1:18080 --session-ttl 5m
~~~

Na máquina do navegador, abra um túnel com `ssh -N -L 18080:127.0.0.1:18080 operador@vm`. Abra `http://127.0.0.1:18080` e confirme que a interface e `/api/v1/` usam a mesma origem. A VM executa somente o binário Go. Registre a versão do Ubuntu e do Chromium, arquitetura, tamanho da janela e tempo desde o início da coleta.

## 2. Sessão

1. Abra `/host/network?range=6h` sem sessão. Confirme o redirecionamento para login e o foco em Usuário. Use Tab para chegar à senha, alterne Mostrar/Ocultar senha e envie via Enter.
2. Envie uma senha incorreta. Confirme o erro genérico e o foco na mensagem. Em uma janela separada de teste, tente até receber 429. A ação fica bloqueada durante o tempo de `Retry-After`. Aguarde o reinício da janela e faça login como `admin`.
3. Confirme o retorno a Rede com 6h marcado. No painel de Cookies do navegador, confirme `HttpOnly`, `SameSite=Strict` e `Path=/api/v1`. Em HTTPS, confirme também `Secure`.
4. Abra Settings e acione Encerrar sessão. Pressione Escape e verifique o retorno do foco ao botão. Reabra, confirme Sair e use Voltar no navegador. Confirme que o conteúdo privado volta a exigir login e que a solicitação de stream anterior termina.
5. Entre novamente e deixe a sessão expirar após 5 minutos. Confirme o aviso de sessão expirada, o fechamento do stream e o retorno ao mesmo contexto após novo login. Para validar revogação administrativa antes do vencimento, execute `./vektord revoke-sessions --data-dir "$vk_test_state"` em outro terminal no mesmo diretório. Espere a revalidação do stream.

## 3. Overview e subsistemas do Host

| Tela | Conferência |
| --- | --- |
| Overview | Compare hostname, SO, kernel e uptime com o host. Compare saúde com `GET /api/v1/health`. Verifique CPU e RAM ao vivo, a montagem `/` se monitorada e a rede agregada conforme o catálogo. |
| CPU | Compare modelo, threads e cores com `lscpu`. Confira carga média em escala própria e IDs da tabela com `/api/v1/resources?kind=cpu_core&state=all`. |
| Memória | Confira unidade de capacidade, RAM disponível, cache, buffers e swap. Uma máquina sem swap informa a condição quando o backend fornece total zero. |
| Armazenamento | Compare mounts com `findmnt` e os dispositivos com `lsblk`. Abra um mount e um disco para conferir ID, origem ou modelo e o histórico aplicável. |
| Rede | Compare interfaces com `ip -brief link`, selecione duas interfaces e confirme IDs e taxas distintas. Veja erros e descartes em unidade/s. |
| Sensores | Confira chip e canal quando disponíveis. A lista pode estar vazia na VM. Se houver sensor com erro de leitura, compare com `/api/v1/metrics`. Um valor medido igual a zero deve permanecer visível. |
| Settings | Altere a preferência da barra lateral e o período padrão. Recarregue e confirme a persistência. Abra `/overview` em nova aba para conferir o padrão salvo. |

Consulte as respostas no painel Network do navegador. Um cenário com hardware ausente é válido. Registre quais recursos reais estavam disponíveis.

## 4. Histórico

1. Aguarde a coleta e o fechamento de pelo menos um bucket de 30 s. Escolha 15m, 1h, 6h, 24h e 7d. Verifique `step=auto`, `max_points=600`, intervalo solicitado e resolução efetiva na resposta e no painel.
2. Selecione Personalizado, informe uma janela UTC válida e copie o link. Valores invertidos e janelas maiores que 90 dias devem ser recusados. Use as setas no controle de um gráfico e confirme a sincronização do cursor entre gráficos com intervalo compartilhado. Confira timestamp UTC, valor, unidade, contagem de amostras e cobertura do bucket. Compare com a resposta REST.
3. Verifique seleção de períodos com Voltar/Avançar, mudança de aba, URL copiada e reload. Teste um detalhe com `resource` na URL e verifique que a troca do período mantém o ID.
4. Gere uma lacuna com uma parada controlada do daemon por pelo menos dois buckets de 30 s. Reinicie usando o mesmo diretório. O gráfico deve interromper o traço no intervalo ausente e mostrar “Sem amostra” no cursor. Um bucket com média zero aparece como medição. O teste automatizado usa fixtures para tornar reproduzíveis cobertura parcial e `422 point_limit`.

## 5. SSE, acessibilidade e sessão longa

1. Com uma única aba autenticada, confirme uma solicitação GET aberta para `/api/v1/stream/live` e `active_live_subscribers: 1`. Mude de tela e confira que o mesmo stream permanece. Verifique que o histórico mantém o ritmo de polling mesmo quando chegam snapshots.
2. Use Offline em DevTools. Confirme o estado Offline e o rótulo de atraso nos últimos valores. Reative a rede e verifique que volta para Ao vivo. Para observar stale com o daemon aberto, suspenda-o temporariamente em um terminal de teste com `kill -STOP PID`, espere aproximadamente 23 segundos e retome com `kill -CONT PID`. Use o PID do daemon de teste.
3. Teste 1440, 1024, 768, 390 e 360 px, com teclado e ponteiro. Use Tab para o link de pular conteúdo, menu, período, tabelas e logout. Use setas para trocar as abas de Host e explorar os gráficos. Confirme foco visível e fechamento do drawer com Escape. Verifique o scroll horizontal dentro das tabelas, em especial Rede. Ative a preferência de movimento reduzido e amplie a página a 200%.
4. Reinicie o daemon de teste com TTL maior, por exemplo `--session-ttl 24h`. Mantenha a Overview aberta por duas horas, alternando Host, tabs e períodos algumas vezes. Anote heap após GC, documentos, nós DOM, listeners e contagem de streams nos minutos 0, 15, 60 e 120. Confira saída do teste de soak automatizado para comparação numérica.

Na máquina de desenvolvimento, com Chromium do Playwright já instalado:

~~~bash
VEKTOR_SOAK_SECONDS=7200 bun run --cwd web test:e2e soak.spec.ts
~~~

Para usar a VM nesse teste com a conta de teste preparada e túnel aberto, defina também `VEKTOR_E2E_URL=http://127.0.0.1:18080` e injete `VEKTOR_E2E_PASSWORD` de forma privada. O teste imprime as amostras de memória, listeners, assinantes e duração de carregamento no terminal. Registre o resultado sem copiar senhas, cookies ou tokens CSRF. Encerre o daemon com Ctrl+C e remova o diretório descartável após registrar os resultados.
