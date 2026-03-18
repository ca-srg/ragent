[Unit]
Description=RAGent Slack Bot Service
After=network.target ragent-init.service
Requires=ragent-init.service

[Service]
Type=simple
User=root
WorkingDirectory=/root
EnvironmentFile=/etc/default/ragent
ExecStart=/usr/local/bin/ragent slack-bot --context-size 10
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
