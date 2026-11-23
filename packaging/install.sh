#!/usr/bin/env bash
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "Execute como root (ex.: sudo $0 ./bin/vektord)" >&2
  exit 1
fi

BINARY="${1:-./bin/vektord}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ ! -x "${BINARY}" ]]; then
  echo "Binário não encontrado ou sem permissão de execução: ${BINARY}" >&2
  echo "Compile primeiro com: bun run --cwd web build && CGO_ENABLED=0 go build -tags webui -o ./bin/vektord ./cmd/vektord" >&2
  exit 1
fi

if ! id -u vektor >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/vektor --shell /usr/sbin/nologin --user-group vektor
fi

install -d -m 0750 -o vektor -g vektor /var/lib/vektor
install -d -m 0750 -o root -g vektor /etc/vektor
install -m 0755 "${BINARY}" /usr/bin/vektord

if [[ ! -f /etc/vektor/vektord.env ]]; then
  install -m 0640 -o root -g vektor "${SCRIPT_DIR}/systemd/vektord.env" /etc/vektor/vektord.env
fi

install -m 0644 "${SCRIPT_DIR}/systemd/vektord.service" /etc/systemd/system/vektord.service
systemctl daemon-reload

echo ""
echo "Vektor instalado em /usr/bin/vektord."
echo "1. Crie a senha do administrador (primeira instalação):"
echo "   sudo -u vektor /usr/bin/vektord bootstrap-admin --data-dir /var/lib/vektor"
echo "2. Ative e inicie o serviço:"
echo "   sudo systemctl enable --now vektord"
echo "3. Verifique o status:"
echo "   sudo systemctl status vektord"
