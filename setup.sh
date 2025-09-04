#!/usr/bin/env bash
# setup.sh - Interactive setup for OpenSearch + Bedrock + S3 Vectors (AWS CLI + SigV4 curl)
set -euo pipefail

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "ERROR: '$1' not found in PATH"; exit 1; }
}
require aws
require curl

# -------- Helpers --------
ask() {
  local prompt="$1"; local def="${2-}"; local showdef=""
  [ -n "$def" ] && showdef=" [$def]"
  read -r -p "$prompt$showdef: " REPLY
  if [ -z "$REPLY" ] && [ -n "$def" ]; then REPLY="$def"; fi
}

confirm() {
  local msg="${1:-Proceed?}"
  read -r -p "$msg [y/N]: " ans; case "${ans,,}" in y|yes) return 0;; *) return 1;; esac
}

sts_acct() { aws sts get-caller-identity --query 'Account' --output text 2>/dev/null || true; }
basename_arn() { printf "%s\n" "$1" | awk -F'/' '{print $NF}'; }

os_curl() {
  # os_curl METHOD PATH [extra curl args...]
  local method="${1:-GET}"; shift
  local path="${1:-/}"; shift
  local args=(
    --silent --show-error --fail
    --aws-sigv4 "aws:amz:${AWS_REGION}:es"
    --user "${AKID}:${SECRET}"
    -H "host: ${OS_HOST}"
    -H "Content-Type: application/json"
    -X "$method" "${OS_EP}${path}"
  )
  if [ -n "${SESSION:-}" ]; then args+=(-H "x-amz-security-token: ${SESSION}"); fi
  curl "${args[@]}" "$@"
}

wait_domain_apply() {
  echo "Waiting for domain config to apply..."
  until [ "$(aws opensearch describe-domain --region "$AWS_REGION" --domain-name "$OS_DOMAIN" \
              --query 'DomainStatus.Processing' --output text)" = "False" ]; do
    sleep 6
    printf "."
  done
  echo " done"
}

# -------- Gather inputs --------
echo "== AWS account and region inputs =="
ACCT_DEF="$(sts_acct)"; ask "AWS Account ID" "${ACCT_DEF:-}"
ACCOUNT_ID="$REPLY"
ask "OpenSearch domain name" "rag"; OS_DOMAIN="$REPLY"
ask "OpenSearch region" "ap-northeast-1"; AWS_REGION="$REPLY"

echo "== OpenSearch endpoint selection =="
ask "Resolve VPC endpoint from domain via AWS CLI? (y/N)" "y"; RESOLVE="${REPLY,,}"
if [ "$RESOLVE" = "y" ] || [ "$RESOLVE" = "yes" ]; then
  OS_HOST="$(aws opensearch describe-domain --region "$AWS_REGION" --domain-name "$OS_DOMAIN" \
            --query 'DomainStatus.Endpoints.vpc' --output text)"
  if [ -z "$OS_HOST" ] || [ "$OS_HOST" = "None" ]; then
    echo "ERROR: Could not resolve VPC endpoint. Provide manually."
    ask "OpenSearch VPC endpoint host (e.g., vpc-xxxx.es.amazonaws.com)" ""; OS_HOST="$REPLY"
  fi
else
  ask "OpenSearch VPC endpoint host (e.g., vpc-xxxx.es.amazonaws.com)" ""; OS_HOST="$REPLY"
fi

ask "Use local port-forward (https://localhost:9200)? (y/N)" "y"; USE_PF="${REPLY,,}"
if [ "$USE_PF" = "y" ] || [ "$USE_PF" = "yes" ]; then
  OS_EP="https://localhost:9200"
  echo "Note: Ensure Host/SNI is '${OS_HOST}' in SigV4 curl (this script sets Host header)."
else
  OS_EP="https://${OS_HOST}"
fi

echo "== Principals (IAM role ARNs) =="
ask "Master security admin role ARN (MasterUserARN). Leave empty to skip" ""
ROLE_MASTER_ARN="${REPLY:-}"
ask "RAG compute role ARN (e.g., EC2/SSM role)" ""
ROLE_RAG_ARN="$REPLY"
ask "Additional admin role ARN to allow (optional)" ""
ROLE_EXTRA_ARN="${REPLY:-}"

echo "== Index and roles =="
ask "OpenSearch index name (for RAG docs)" "kibela"; OS_INDEX="$REPLY"
ask "Grant temporary 'all_access' to RAG role for bulk troubleshooting? (y/N)" "y"; ADD_ALL_ACCESS="${REPLY,,}"

echo "== S3 Vectors =="
ask "S3 Vectors region" "us-east-1"; S3V_REGION="$REPLY"
ask "S3 Vectors bucket name" "rag"; S3V_BUCKET="$REPLY"
ask "S3 Vectors index name" "$OS_INDEX"; S3V_INDEX="$REPLY"

echo "== Bedrock (models and region) =="
ask "Bedrock region" "us-east-1"; BDR_REGION="$REPLY"
ask "Grant UNRESTRICTED Bedrock invoke (all models, all regions)? (y/N)" "y"; BDR_UNRESTRICTED="${REPLY,,}"
ask "Embedding model ID" "amazon.titan-embed-text-v2:0"; EMB_MODEL_ID="$REPLY"
ask "Chat model ID (optional, leave empty to skip)" "anthropic.claude-3-5-sonnet-20240620-v1:0"; CHAT_MODEL_ID="$REPLY"

# -------- Show plan and confirm --------
cat <<PLAN

Plan:
- OpenSearch domain: arn:aws:es:${AWS_REGION}:${ACCOUNT_ID}:domain/${OS_DOMAIN}
- OpenSearch host:  ${OS_HOST}
- OpenSearch entry: ${OS_EP}
- MasterUserARN:    ${ROLE_MASTER_ARN:-"(skip)"}
- Allow principals: ${ROLE_RAG_ARN}${ROLE_EXTRA_ARN:+, ${ROLE_EXTRA_ARN}}
- Create/Update role: kibela_rag_role (cluster:monitor/health + CRUD/bulk on ${OS_INDEX}*)
- Map backend_roles: [RAG role${ROLE_EXTRA_ARN:+, Extra role}]
- Create index if missing: ${OS_INDEX}
- Temp map all_access to RAG role: ${ADD_ALL_ACCESS}
- IAM put-role-policy on $(basename_arn "${ROLE_RAG_ARN}"):
  - Bedrock: ${BDR_UNRESTRICTED}
    - If restricted, region=${BDR_REGION}, models=${EMB_MODEL_ID}${CHAT_MODEL_ID:+, ${CHAT_MODEL_ID}}
  - S3 Vectors (${S3V_REGION}): bucket=${S3V_BUCKET}, index=${S3V_INDEX}
PLAN

confirm "Proceed with the above plan?" || { echo "Aborted."; exit 1; }

# -------- Resolve credentials for SigV4 curl --------
AKID="$(aws configure get aws_access_key_id || true)"
SECRET="$(aws configure get aws_secret_access_key || true)"
SESSION="$(aws configure get aws_session_token || true)"
[ -z "$AKID" ] && { echo "ERROR: No AWS credentials configured"; exit 1; }

echo "== Caller identity =="
aws sts get-caller-identity || true

# -------- 1) Update domain access policy --------
PRINCIPALS="\"${ROLE_RAG_ARN}\""
[ -n "$ROLE_EXTRA_ARN" ] && PRINCIPALS="${PRINCIPALS},\"${ROLE_EXTRA_ARN}\""
POLICY_JSON=$(cat <<JSON
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": { "AWS": [ ${PRINCIPALS} ] },
    "Action": "es:*",
    "Resource": "arn:aws:es:${AWS_REGION}:${ACCOUNT_ID}:domain/${OS_DOMAIN}/*"
  }]
}
JSON
)
aws opensearch update-domain-config \
  --region "$AWS_REGION" --domain-name "$OS_DOMAIN" \
  --access-policies "$POLICY_JSON" >/dev/null

# -------- 2) Set MasterUserARN (optional) --------
if [ -n "$ROLE_MASTER_ARN" ]; then
  aws opensearch update-domain-config \
    --region "$AWS_REGION" --domain-name "$OS_DOMAIN" \
    --advanced-security-options "MasterUserOptions={MasterUserARN=\"${ROLE_MASTER_ARN}\"}" >/dev/null || true
fi

# -------- 3) Wait for apply --------
wait_domain_apply

# -------- 4) Create/Update kibela_rag_role --------
ROLE_BODY=$(cat <<'JSON'
{
  "cluster_permissions": [ "cluster:monitor/health" ],
  "index_permissions": [{
    "index_patterns": ["kibela*"],
    "allowed_actions": [
      "crud",
      "create_index",
      "indices:data/write/bulk",
      "indices:data/write/index",
      "indices:admin/mapping/put",
      "indices:admin/exists",
      "indices:admin/get",
      "indices:monitor/*"
    ]
  }]
}
JSON
)
# Replace index pattern with chosen index prefix
ROLE_BODY="${ROLE_BODY//kibela*/${OS_INDEX}*}"
printf '%s' "$ROLE_BODY" > /tmp/kibela_role.json
os_curl PUT "/_plugins/_security/api/roles/kibela_rag_role" --data @/tmp/kibela_role.json || true

# -------- 5) Role mapping for kibela_rag_role --------
MAP_BODY=$(cat <<JSON
{"backend_roles":["${ROLE_RAG_ARN}"${ROLE_EXTRA_ARN:+,\"${ROLE_EXTRA_ARN}\"}],"hosts":[],"users":[]}
JSON
)
printf '%s' "$MAP_BODY" > /tmp/kibela_rolesmapping.json
os_curl PUT "/_plugins/_security/api/rolesmapping/kibela_rag_role" --data @/tmp/kibela_rolesmapping.json || true

# -------- 6) Create index if missing --------
INDEX_BODY=$(cat <<'JSON'
{
  "settings": { "index": { "knn": true } },
  "mappings": {
    "properties": {
      "title":   { "type":"text", "analyzer":"kuromoji" },
      "content": { "type":"text", "analyzer":"kuromoji" },
      "body":    { "type":"text", "analyzer":"kuromoji" },
      "category":   { "type":"keyword" },
      "tags":       { "type":"keyword" },
      "created_at": { "type":"date"    },
      "updated_at": { "type":"date"    },
      "embedding": {
        "type":"knn_vector",
        "dimension": 1024,
        "method": { "engine":"lucene", "space_type":"cosinesimil", "name":"hnsw", "parameters": {} }
      }
    }
  }
}
JSON
)
printf '%s' "$INDEX_BODY" > /tmp/os_index.json
if ! os_curl HEAD "/${OS_INDEX}" >/dev/null 2>&1; then
  os_curl PUT "/${OS_INDEX}" --data @/tmp/os_index.json
fi

# -------- 7) Temporary: map all_access to RAG role (optional) --------
if [ "$ADD_ALL_ACCESS" = "y" ] || [ "$ADD_ALL_ACCESS" = "yes" ]; then
  printf '{"backend_roles":["%s"],"hosts":[],"users":[]}\n' "$ROLE_RAG_ARN" > /tmp/all_access_rolesmapping.json
  os_curl PUT "/_plugins/_security/api/rolesmapping/all_access" --data @/tmp/all_access_rolesmapping.json || true
fi

# -------- 8) IAM inline policies (Bedrock + S3 Vectors) --------
ROLE_RAG_NAME="$(basename_arn "$ROLE_RAG_ARN")"

# Bedrock
if [ "$BDR_UNRESTRICTED" = "y" ] || [ "$BDR_UNRESTRICTED" = "yes" ]; then
  cat > /tmp/bedrock-invoke.json <<'JSON'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "BedrockUnrestrictedInvoke",
      "Effect": "Allow",
      "Action": [
        "bedrock:InvokeModel",
        "bedrock:InvokeModelWithResponseStream",
        "bedrock:Converse",
        "bedrock:ConverseStream"
      ],
      "Resource": [
        "arn:aws:bedrock:*::foundation-model/*"
      ]
    },
    {
      "Sid": "BedrockModelDiscovery",
      "Effect": "Allow",
      "Action": [
        "bedrock:ListFoundationModels",
        "bedrock:GetFoundationModel"
      ],
      "Resource": "*"
    }
  ]
}
JSON
else
  BDR_STATEMENTS=""
  if [ -n "$EMB_MODEL_ID" ]; then
    BDR_STATEMENTS="${BDR_STATEMENTS}{\"Sid\":\"InvokeEmb\",\"Effect\":\"Allow\",\"Action\":[\"bedrock:InvokeModel\",\"bedrock:InvokeModelWithResponseStream\"],\"Resource\":[\"arn:aws:bedrock:${BDR_REGION}::foundation-model/${EMB_MODEL_ID}\"]}"
  fi
  if [ -n "$CHAT_MODEL_ID" ]; then
    [ -n "$BDR_STATEMENTS" ] && BDR_STATEMENTS="${BDR_STATEMENTS},"
    BDR_STATEMENTS="${BDR_STATEMENTS}{\"Sid\":\"InvokeChat\",\"Effect\":\"Allow\",\"Action\":[\"bedrock:InvokeModel\",\"bedrock:InvokeModelWithResponseStream\"],\"Resource\":[\"arn:aws:bedrock:${BDR_REGION}::foundation-model/${CHAT_MODEL_ID}\"]}"
  fi
  printf '{ "Version":"2012-10-17", "Statement":[%s] }\n' "$BDR_STATEMENTS" > /tmp/bedrock-invoke.json
fi
aws iam put-role-policy --role-name "$ROLE_RAG_NAME" --policy-name AllowBedrockInvoke --policy-document file:///tmp/bedrock-invoke.json

# S3 Vectors
cat > /tmp/s3vectors-inline.json <<JSON
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "S3VectorsBucketAccess",
      "Effect": "Allow",
      "Action": ["s3vectors:GetVectorBucket","s3vectors:ListIndexes"],
      "Resource": "arn:aws:s3vectors:${S3V_REGION}:${ACCOUNT_ID}:bucket/${S3V_BUCKET}"
    },
    {
      "Sid": "S3VectorsIndexAccess",
      "Effect": "Allow",
      "Action": ["s3vectors:GetIndex","s3vectors:CreateIndex","s3vectors:DeleteIndex","s3vectors:PutVectors","s3vectors:DeleteVectors","s3vectors:ListVectors","s3vectors:GetVectors","s3vectors:QueryVectors"],
      "Resource": "arn:aws:s3vectors:${S3V_REGION}:${ACCOUNT_ID}:bucket/${S3V_BUCKET}/index/${S3V_INDEX}"
    }
  ]
}
JSON
aws iam put-role-policy --role-name "$ROLE_RAG_NAME" --policy-name "AllowS3Vectors_${S3V_BUCKET}_${S3V_INDEX}" --policy-document file:///tmp/s3vectors-inline.json

# -------- 9) Checks --------
echo "== OpenSearch account roles =="
os_curl GET "/_plugins/_security/api/account" || true
echo
echo "== Cluster health =="
os_curl GET "/_cluster/health?pretty" || true
echo
echo "== Index list =="
os_curl GET "/_cat/indices/${OS_INDEX}?v" || true
echo
echo "Setup completed."
