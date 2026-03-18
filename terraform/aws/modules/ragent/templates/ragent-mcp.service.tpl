[Unit]
Description=RAGent MCP Server Service
After=network.target ragent-init.service
Requires=ragent-init.service

[Service]
Type=simple
User=root
WorkingDirectory=/root
EnvironmentFile=/etc/default/ragent
ExecStart=/usr/local/bin/ragent mcp-server --host 0.0.0.0 --auth-method ${mcp_auth_method}%{ for cidr in mcp_bypass_ip_ranges ~} --bypass-ip-range ${cidr}%{ endfor ~}
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
