# OpenID Connect Authentication for MCP Server

This document describes how to configure and use OpenID Connect (OIDC) authentication for the MCP server.

## Overview

The MCP server now supports OpenID Connect authentication in addition to the existing IP-based authentication. This allows for browser-based authentication flows and integration with enterprise identity providers. OIDC機能は`mcp-server`コマンドに統合されました。

Note: Since v2.x, the OAuth2 callback endpoint `/callback` is served by the same MCP server process on `MCP_SERVER_PORT`. No separate callback listener or port option is required. You can initiate authentication at `/login`.

## Authentication Methods

The server supports four authentication methods:

- `ip` - Traditional IP address-based authentication only
- `oidc` - OpenID Connect authentication only
- `both` - Requires both IP and OIDC authentication
- `either` - Allows either IP or OIDC authentication

## Configuration

### Environment Variables

Set the following environment variables for OIDC configuration:

```bash
# OIDC Provider Configuration (with discovery)
export OIDC_ISSUER="https://accounts.google.com"  # Your OIDC provider URL
export OIDC_CLIENT_ID="your-client-id"            # OAuth2 client ID
export OIDC_CLIENT_SECRET="your-client-secret"    # OAuth2 client secret (optional for public clients)

# Custom OIDC Endpoints (optional - overrides discovery)
export OIDC_AUTH_URL="https://custom.provider.com/oauth/authorize"  # Custom authorization endpoint
export OIDC_TOKEN_URL="https://custom.provider.com/oauth/token"     # Custom token endpoint
export OIDC_USERINFO_URL="https://custom.provider.com/oauth/userinfo" # Custom userinfo endpoint
export OIDC_JWKS_URL="https://custom.provider.com/.well-known/jwks.json" # Custom JWKS endpoint

# Optional: OpenSearch and AWS configuration (same as before)
export OPENSEARCH_ENDPOINT="https://your-opensearch-domain.region.es.amazonaws.com"
export OPENSEARCH_INDEX="ragent-docs"
export AWS_REGION="us-east-1"

# MCP server port (also used for OAuth2 callback)
export MCP_SERVER_PORT=8080
```

### Command Line Options

Start the MCP server with OIDC authentication (now unified into `mcp-server`):

```bash
# OIDC authentication only
./ragent mcp-server --auth-method oidc

# Allow either IP or OIDC (recommended for development)
./ragent mcp-server --auth-method either

# Require both IP and OIDC (highest security)
./ragent mcp-server --auth-method both

# Custom OIDC configuration with discovery
./ragent mcp-server \
  --auth-method oidc \
  --oidc-issuer "https://login.microsoftonline.com/{tenant-id}/v2.0" \
  --oidc-client-id "your-app-id" \
  --oidc-client-secret "your-secret"

# Custom OIDC endpoints without discovery
./ragent mcp-server \
  --auth-method oidc \
  --oidc-client-id "your-app-id" \
  --oidc-client-secret "your-secret" \
  --oidc-auth-url "https://auth.example.com/oauth/authorize" \
  --oidc-token-url "https://auth.example.com/oauth/token" \
  --oidc-userinfo-url "https://auth.example.com/oauth/userinfo" \
  --oidc-skip-discovery

# Mix discovery with custom endpoint overrides
./ragent mcp-server \
  --auth-method oidc \
  --oidc-issuer "https://accounts.google.com" \
  --oidc-client-id "your-client-id" \
  --oidc-auth-url "https://custom.auth.endpoint/authorize"
```

## Custom Endpoint Configuration

The MCP server supports three modes of OIDC configuration:

### 1. Full OIDC Discovery (Recommended)
Uses the standard OIDC discovery mechanism to automatically configure all endpoints:
```bash
./ragent mcp-server \
  --oidc-issuer "https://accounts.google.com" \
  --oidc-client-id "your-client-id"
```

### 2. Custom Endpoints Only
Bypasses OIDC discovery and uses only manually configured endpoints:
```bash
./ragent mcp-server \
  --oidc-skip-discovery \
  --oidc-client-id "your-client-id" \
  --oidc-auth-url "https://auth.example.com/authorize" \
  --oidc-token-url "https://auth.example.com/token"
```

### 3. Discovery with Overrides
Uses OIDC discovery but overrides specific endpoints:
```bash
./ragent mcp-server \
  --oidc-issuer "https://accounts.google.com" \
  --oidc-client-id "your-client-id" \
  --oidc-token-url "https://custom.token.endpoint/token"
```

## Supported Identity Providers

The implementation has been designed to work with major identity providers:

### Google Workspace
```bash
export OIDC_ISSUER="https://accounts.google.com"
export OIDC_CLIENT_ID="your-google-client-id.apps.googleusercontent.com"
export OIDC_CLIENT_SECRET="your-google-client-secret"
```

### Microsoft Azure AD / Entra ID
```bash
export OIDC_ISSUER="https://login.microsoftonline.com/{tenant-id}/v2.0"
export OIDC_CLIENT_ID="your-application-id"
export OIDC_CLIENT_SECRET="your-client-secret"
```

### Okta
```bash
export OIDC_ISSUER="https://{your-domain}.okta.com"
export OIDC_CLIENT_ID="your-okta-client-id"
export OIDC_CLIENT_SECRET="your-okta-client-secret"
```

### Keycloak
```bash
export OIDC_ISSUER="https://{keycloak-server}/realms/{realm}"
export OIDC_CLIENT_ID="your-keycloak-client"
export OIDC_CLIENT_SECRET="your-keycloak-secret"
```

### Custom OAuth2 Provider
For providers that don't support OIDC discovery:
```bash
export OIDC_CLIENT_ID="your-client-id"
export OIDC_CLIENT_SECRET="your-client-secret"
export OIDC_AUTH_URL="https://oauth.provider.com/authorize"
export OIDC_TOKEN_URL="https://oauth.provider.com/token"
export OIDC_USERINFO_URL="https://oauth.provider.com/userinfo"

# Run with skip-discovery flag
./ragent mcp-server-oidc --oidc-skip-discovery
```

## Authentication Flow

1. **Initial Request**: When a client makes a request to the MCP server without authentication
2. **Authentication Required**: Server returns a 401 response with an authentication URL
3. **Browser Authentication**: 
   - For CLI/desktop clients: Browser automatically opens to the authentication URL
   - For API clients: Authentication URL is provided in the error response
4. **OAuth2 Callback**: After successful authentication, the browser is redirected to the callback URL (http://<server-host>[:${MCP_SERVER_PORT}]/callback). The page will display a command to add this MCP server to Claude CLI with the issued JWT, e.g.:

```
claude mcp add --transport sse ragent https://your-server.example.com --header "Authorization: Bearer <JWT>"
```

Visit `http://<server-host>[:${MCP_SERVER_PORT}]/login` to start authentication.

### Transports

The server exposes dedicated endpoints for each transport:

- HTTP: `claude mcp add --transport http private-api https://your-server.example.com/mcp --header "Authorization: Bearer <JWT>"`
- SSE:  `claude mcp add --transport sse  private-api https://your-server.example.com/sse --header "Authorization: Bearer <JWT>"`

SSE uses a hanging GET to `/sse` with `Accept: text/event-stream`, and POSTs to `/sse?sessionid=...`.
5. **Token Exchange**: The authorization code is exchanged for ID and access tokens
6. **Token Validation**: The ID token is validated and stored
7. **Authenticated Access**: Subsequent requests include the token for authentication

## Token Management

### Token Storage
- Tokens are stored in memory with automatic expiration tracking
- No persistent storage is used by default for security

### Token Validation
- ID tokens are validated using the OIDC provider's public keys
- Token expiration is checked on each request
- Signature verification is performed automatically

### Token Usage
Clients can provide authentication tokens in three ways:
1. **Authorization Header**: `Authorization: Bearer <token>`
2. **Query Parameter**: `?token=<token>`
3. **Cookie**: `mcp_auth_token=<token>`

## Security Considerations

1. **HTTPS Required**: Always use HTTPS in production for the OIDC provider
2. **Client Secret**: Store client secrets securely, never commit to version control
3. **Callback URL**: Use `localhost` for development, proper domain for production
4. **Token Expiration**: Tokens expire based on the OIDC provider's configuration
5. **PKCE**: The implementation uses PKCE (Proof Key for Code Exchange) for added security

## API Response Format

When authentication is required, the server returns:

```json
{
  "jsonrpc": "2.0",
  "error": {
    "code": -32001,
    "message": "Authentication required",
    "data": {
      "auth_url": "https://provider.com/authorize?client_id=...&redirect_uri=..."
    }
  }
}
```

## Troubleshooting

### Common Issues

1. **Browser doesn't open automatically**
   - Check system permissions for opening browsers
   - Manually visit the URL shown in the logs

2. **Callback fails**
   - Ensure MCP_SERVER_PORT is not in use
   - Check firewall settings for localhost connections

3. **Token validation fails**
   - Verify the OIDC issuer URL is correct
   - Check network connectivity to the OIDC provider
   - Ensure system time is synchronized

### Debug Logging

Enable detailed logging for troubleshooting:

```bash
./ragent mcp-server --auth-enable-logging --auth-method oidc
```

## Integration with MCP Clients

### Claude Desktop Configuration

Add to your Claude Desktop configuration:

```json
{
  "mcpServers": {
    "ragent": {
      "command": "ragent",
      "args": ["mcp-server", "--auth-method", "oidc"],
      "env": {
        "OIDC_ISSUER": "https://accounts.google.com",
        "OIDC_CLIENT_ID": "your-client-id",
        "OPENSEARCH_ENDPOINT": "https://your-domain.es.amazonaws.com",
        "AWS_REGION": "us-east-1"
      }
    }
  }
}
```

### Programmatic Access

For programmatic access, first obtain a token through the authentication flow, then include it in requests:

```python
import requests

# First, get the auth URL
response = requests.post("http://localhost:8080", json={
    "jsonrpc": "2.0",
    "method": "tools/list",
    "id": 1
})

if response.status_code == 401:
    auth_url = response.json()["error"]["data"]["auth_url"]
    print(f"Please authenticate at: {auth_url}")
    # After authentication, get the token
    token = "your-obtained-token"
    
    # Use the token for subsequent requests
    response = requests.post(
        "http://localhost:8080",
        headers={"Authorization": f"Bearer {token}"},
        json={
            "jsonrpc": "2.0",
            "method": "tools/list",
            "id": 1
        }
    )
```

## Migration from IP Authentication

To migrate from IP-only authentication:

1. **Test with `either` mode**: Start with `--auth-method either` to allow both methods
2. **Configure OIDC**: Set up your OIDC provider and obtain client credentials
3. **Update clients**: Modify client configurations to handle OIDC authentication
4. **Switch to OIDC**: Once tested, switch to `--auth-method oidc` for OIDC-only

## Best Practices

1. **Use `either` mode for development**: Allows flexibility during development
2. **Use `oidc` mode for production**: Provides centralized authentication
3. **Use `both` mode for high security**: Combines network and identity security
4. **Regular token rotation**: Configure appropriate token lifetimes in your OIDC provider
5. **Monitor authentication logs**: Enable logging to track authentication attempts
