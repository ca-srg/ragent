[Unit]
Description=RAGent OpenSearch Index Initialization
After=network.target%{ if is_docker_opensearch } docker.service%{ endif }
%{ if is_docker_opensearch ~}
Requires=docker.service
%{ endif ~}

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/var/lib/ragent/init-opensearch.sh
TimeoutStartSec=180
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
