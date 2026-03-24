#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_ROOT="${TARISH_SERVER_INSTALL_ROOT:-/opt/tarish/server}"
SERVICE_PATH="${TARISH_SERVER_SERVICE_PATH:-/etc/systemd/system/tarish-server.service}"
ENV_PATH="${TARISH_SERVER_ENV_PATH:-/etc/tarish-server.env}"
DB_DIR="${TARISH_SERVER_DB_DIR:-/var/lib/tarish}"
SERVICE_USER="${TARISH_SERVER_USER:-tarish}"
SERVICE_GROUP="${TARISH_SERVER_GROUP:-tarish}"

usage() {
    cat <<EOF
Usage: sudo ./install.sh

Install the packaged tarish-server bundle into:
  Binary and docs: ${INSTALL_ROOT}
  systemd unit:    ${SERVICE_PATH}
  Env file:        ${ENV_PATH}
  SQLite data dir: ${DB_DIR}

Optional environment overrides:
  TARISH_SERVER_INSTALL_ROOT
  TARISH_SERVER_SERVICE_PATH
  TARISH_SERVER_ENV_PATH
  TARISH_SERVER_DB_DIR
  TARISH_SERVER_USER
  TARISH_SERVER_GROUP
EOF
}

ensure_service_account() {
    if ! getent group "${SERVICE_GROUP}" >/dev/null 2>&1; then
        echo "Creating system group ${SERVICE_GROUP}"
        groupadd --system "${SERVICE_GROUP}"
    fi

    if ! id -u "${SERVICE_USER}" >/dev/null 2>&1; then
        echo "Creating system user ${SERVICE_USER}"
        useradd \
            --system \
            --home-dir "${DB_DIR}" \
            --no-create-home \
            --shell /usr/sbin/nologin \
            --gid "${SERVICE_GROUP}" \
            "${SERVICE_USER}"
    fi
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
    usage
    exit 0
fi

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: run this installer as root, for example: sudo ./install.sh" >&2
    exit 1
fi

for required_file in tarish-server tarish-server.service tarish-server.env.example README.md; do
    if [ ! -f "${SCRIPT_DIR}/${required_file}" ]; then
        echo "Error: missing ${required_file} in ${SCRIPT_DIR}" >&2
        echo "Run this script from the extracted release bundle directory." >&2
        exit 1
    fi
done

ensure_service_account

echo "Installing tarish-server into ${INSTALL_ROOT}"
install -d -m 0755 "${INSTALL_ROOT}"
install -d -m 0755 "${DB_DIR}"
chown "${SERVICE_USER}:${SERVICE_GROUP}" "${DB_DIR}"

install -m 0755 "${SCRIPT_DIR}/tarish-server" "${INSTALL_ROOT}/tarish-server"
install -m 0644 "${SCRIPT_DIR}/tarish-server.service" "${SERVICE_PATH}"
install -m 0644 "${SCRIPT_DIR}/README.md" "${INSTALL_ROOT}/README.md"

if [ -f "${SCRIPT_DIR}/cloudflared.example.yml" ]; then
    install -m 0644 "${SCRIPT_DIR}/cloudflared.example.yml" "${INSTALL_ROOT}/cloudflared.example.yml"
fi

if [ -f "${SCRIPT_DIR}/VERSION" ]; then
    install -m 0644 "${SCRIPT_DIR}/VERSION" "${INSTALL_ROOT}/VERSION"
fi

if [ -f "${ENV_PATH}" ]; then
    echo "Keeping existing env file at ${ENV_PATH}"
else
    install -m 0644 "${SCRIPT_DIR}/tarish-server.env.example" "${ENV_PATH}"
    echo "Created ${ENV_PATH} from tarish-server.env.example"
fi

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
fi

cat <<EOF

Install complete.

Next steps:
  1. Edit ${ENV_PATH} and replace CHANGE_ME.
  2. Start the service:
       sudo systemctl enable --now tarish-server
  3. Check status:
       sudo systemctl status tarish-server
EOF
