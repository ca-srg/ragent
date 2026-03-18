[Unit]
Description=RAGent Vectorize Service
After=network.target ragent-init.service
Requires=ragent-init.service

[Service]
Type=simple
User=root
WorkingDirectory=/root
EnvironmentFile=/etc/default/ragent
ExecStart=/usr/local/bin/ragent vectorize%{ if vectorize_s3_source_bucket != null } --enable-s3 --s3-bucket ${vectorize_s3_source_bucket}%{ endif }%{ if vectorize_github_repos != null } --github-repos "${vectorize_github_repos}"%{ endif } --follow
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
