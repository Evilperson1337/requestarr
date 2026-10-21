#!/usr/bin/env bash
set -euo pipefail

host_modes="/home/vscode/.zoo-host/custom_modes.yaml"
roo_home="/home/vscode/.roo"

global_storage="/home/vscode/.vscode-server/data/User/globalStorage"
zoo_storage="${global_storage}/zoocodeorganization.zoo-code"
settings_dir="${zoo_storage}/settings"
destination_modes="${settings_dir}/custom_modes.yaml"

echo "=== Zoo Code synchronization ==="

if [[ -f "$host_modes" ]]; then
    mkdir -p "$settings_dir"
    cp -f "$host_modes" "$destination_modes"
    echo "Custom modes synchronized: $host_modes -> $destination_modes"
else
    echo "WARNING: Zoo Code custom modes are unavailable: $host_modes" >&2
fi

if [[ -d "${roo_home}/commands" ]]; then
    command_count="$(find "${roo_home}/commands" -type f | wc -l | tr -d ' ')"
    echo "Zoo Code slash commands available: ${command_count}"
else
    echo "WARNING: Zoo Code commands are unavailable: ${roo_home}/commands" >&2
fi
