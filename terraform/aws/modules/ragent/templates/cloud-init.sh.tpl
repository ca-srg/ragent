#!/bin/bash
set -euo pipefail

mkdir -p /etc/default /root/.ragent /root/jsonl /var/lib/ragent

curl -fsSL "${ragent_binary_url}" | tar -xz -C /usr/local/bin
chmod +x /usr/local/bin/ragent

%{ if is_docker_opensearch ~}
dnf install -y docker
systemctl enable --now docker
docker run -d --name opensearch \
  --restart always \
  -p 9200:9200 \
  -e "discovery.type=single-node" \
  -e "DISABLE_SECURITY_PLUGIN=true" \
  opensearchproject/opensearch:2.19.0
%{ endif ~}

cat > /etc/default/ragent <<'ENVFILE'
${ragent_env_file}
ENVFILE

# OpenSearch index initialization script and service
cat > /var/lib/ragent/init-opensearch.sh <<'INITSCRIPT'
${ragent_init_script}
INITSCRIPT
chmod +x /var/lib/ragent/init-opensearch.sh

cat > /etc/systemd/system/ragent-init.service <<'INITUNIT'
${ragent_init_service}
INITUNIT

cat > /etc/systemd/system/ragent-mcp.service <<'MCPUNIT'
${ragent_mcp_service}
MCPUNIT

%{ if slack_bot_enabled ~}
cat > /etc/systemd/system/ragent-slack.service <<'SLACKUNIT'
${ragent_slack_service}
SLACKUNIT
%{ endif ~}

%{ if vectorize_enabled ~}
cat > /etc/systemd/system/ragent-vectorize.service <<'VECUNIT'
${ragent_vectorize_service}
VECUNIT
%{ endif ~}

%{ for service_name, override_content in systemd_service_overrides ~}
mkdir -p /etc/systemd/system/${service_name}.service.d
cat > /etc/systemd/system/${service_name}.service.d/override.conf <<'OVERRIDE'
${override_content}
OVERRIDE
%{ endfor ~}

systemctl daemon-reload
systemctl enable --now ragent-init.service
systemctl enable --now ragent-mcp.service
%{ if slack_bot_enabled ~}
systemctl enable --now ragent-slack.service
%{ endif ~}
%{ if vectorize_enabled ~}
systemctl enable --now ragent-vectorize.service
%{ endif ~}
