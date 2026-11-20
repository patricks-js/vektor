# ADR-005 — Administrador local e sessões SQLite

**Status** Aceita

**Data** 2026-11-20

## Contexto

A API atende um único administrador local. A autenticação precisa sobreviver ao restart, limitar tentativas de login e invalidar sessões quando solicitado.

## Decisão

- `bootstrap-admin` lê a senha via terminal sem eco ou entrada padrão. Uma linha singleton protegida por chave e constraint no SQLite garante uma única criação mesmo com chamadas concorrentes. A migração 003 adiciona as tabelas de autenticação e o daemon restringe o acesso ao arquivo e seus sidecars.
- Argon2id usa três iterações, 64 MiB, quatro lanes, salt aleatório de 16 bytes e saída de 32 bytes. O método compara os digests em tempo constante e faz o mesmo trabalho de derivação antes da criação da conta ou com nome incorreto. O processo admite uma derivação de login simultânea. [RFC 9106, seção 4](https://www.rfc-editor.org/rfc/rfc9106.html#section-4)
- Sessões contêm token opaco de 32 bytes de aleatoriedade. O SQLite armazena SHA-256 do token e expiração absoluta. Logout remove a linha no banco e o comando local `revoke-sessions` revoga todas as sessões. A autenticação consulta o banco a cada requisição.
- O cookie é HttpOnly, SameSite Strict e Secure em HTTPS direto ou origem pública HTTPS. Login exige JSON e origens informadas devem corresponder à origem configurada ou inferida. Logout exige um token CSRF derivado do segredo da sessão em domínio separado. O servidor nunca precisa persistir o token CSRF.
- O limite de tentativas de login usa o IP do socket e um orçamento global. Os streams revalidam a sessão em cada heartbeat e quando o processo revoga uma sessão por logout.

## Consequências

A conta e as sessões sobrevivem a reinicializações e exigem proteção do diretório SQLite e dos backups. A revogação por outro processo é imediata para novas requisições e chega a streams abertos na próxima revalidação. A v0.2 oferece revogação via CLI e bootstrap único. Recuperação de senha e múltiplos administradores ficam para uma decisão futura.
