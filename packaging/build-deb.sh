#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-0.4.0}"
ARCH="${2:-$(dpkg --print-architecture 2>/dev/null || echo amd64)}"
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT

if [[ "${SKIP_WEB_BUILD:-0}" != "1" ]]; then
  echo "Building frontend (web/dist)..."
  bun run --cwd "${ROOT_DIR}/web" build
fi

echo "Building static vektord and vektor-ctl (${VERSION} / ${ARCH})..."
(cd "${ROOT_DIR}" && CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build -tags webui -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o "${STAGE}/usr/bin/vektord" ./cmd/vektord)
(cd "${ROOT_DIR}" && CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o "${STAGE}/usr/bin/vektor-ctl" ./cmd/vektor-ctl)

install -d "${STAGE}/DEBIAN" "${STAGE}/lib/systemd/system" "${STAGE}/etc/vektor"
install -m 0644 "${ROOT_DIR}/packaging/systemd/vektord.service" "${STAGE}/lib/systemd/system/vektord.service"
install -m 0644 "${ROOT_DIR}/packaging/systemd/vektor-ctl.socket" "${STAGE}/lib/systemd/system/vektor-ctl.socket"
install -m 0644 "${ROOT_DIR}/packaging/systemd/vektor-ctl.service" "${STAGE}/lib/systemd/system/vektor-ctl.service"
install -m 0640 "${ROOT_DIR}/packaging/systemd/vektord.env" "${STAGE}/etc/vektor/vektord.env"

cat > "${STAGE}/DEBIAN/conffiles" <<'EOF'
/etc/vektor/vektord.env
EOF

cat > "${STAGE}/DEBIAN/control" <<EOF
Package: vektor
Version: ${VERSION}
Section: admin
Priority: optional
Architecture: ${ARCH}
Maintainer: Vektor Project <noreply@vektor.local>
Description: Lightweight Linux host monitoring daemon and dashboard
 Unprivileged Go daemon collecting Linux telemetry from /proc and /sys,
 persisting multi-resolution history into SQLite WAL, evaluating alerts,
 and serving an embedded web dashboard over HTTP/SSE.
EOF

cat > "${STAGE}/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e
if ! id -u vektor >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/vektor --shell /usr/sbin/nologin --user-group vektor
fi
if getent group systemd-journal >/dev/null 2>&1; then
  usermod -aG systemd-journal vektor || true
fi
install -d -m 0750 -o vektor -g vektor /var/lib/vektor
chown root:vektor /etc/vektor /etc/vektor/vektord.env
chmod 0750 /etc/vektor
chmod 0640 /etc/vektor/vektord.env
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload || true
  systemctl enable --now vektor-ctl.socket || true
  systemctl try-restart vektord || true
fi
echo "Vektor installed. Run: sudo -u vektor /usr/bin/vektord bootstrap-admin --data-dir /var/lib/vektor"
echo "Then enable: sudo systemctl enable --now vektord"
EOF
chmod 0755 "${STAGE}/DEBIAN/postinst"

cat > "${STAGE}/DEBIAN/prerm" <<'EOF'
#!/bin/sh
set -e
if [ "$1" = "remove" ] && command -v systemctl >/dev/null 2>&1; then
  systemctl stop vektord vektor-ctl.socket vektor-ctl.service || true
  systemctl disable vektord vektor-ctl.socket || true
fi
EOF
chmod 0755 "${STAGE}/DEBIAN/prerm"

mkdir -p "${ROOT_DIR}/dist"
OUT="${ROOT_DIR}/dist/vektor_${VERSION}_${ARCH}.deb"
dpkg-deb --build --root-owner-group "${STAGE}" "${OUT}"
echo "Package created: ${OUT}"
