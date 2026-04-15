#!/usr/bin/env python3
"""RAGent MCP Proxy - bridges Claude Desktop (stdio) to RAGent's MCP server (SSE).

This script is the entry point for the MCPB package. When installed via MCPB,
the mcp_config in manifest.json handles execution automatically.

This file can also be run directly:

    RAGENT_SERVER_URL=http://localhost:8080/sse python proxy.py

Requires: uv (https://docs.astral.sh/uv/)
"""

import os
import subprocess
import sys


def main() -> None:
    server_url = os.environ.get("RAGENT_SERVER_URL", "http://localhost:8080/mcp")
    try:
        subprocess.run(
            ["uvx", "mcp-proxy", "--transport", "streamablehttp", server_url],
            check=True,
        )
    except FileNotFoundError:
        print(
            "Error: 'uvx' command not found. Install uv: https://docs.astral.sh/uv/",
            file=sys.stderr,
        )
        sys.exit(1)
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
