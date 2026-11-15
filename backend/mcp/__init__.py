"""
OVH Nexus — Model Context Protocol (MCP) Gateway Package
========================================================
Provides:
  - Streamable HTTP JSON-RPC 2.0 endpoint (/api/mcp)
  - Dedicated Bearer Token authentication & brute-force rate limiting
  - L1 / L2 / L3 permission scope enforcement (L4 & auto-pay permanently blocked)
  - Bidirectional Privacy Shield (alias tokenization + PII/IP/orderUrl stripping)
  - Multi-account visibility whitelist & audit logging
"""

from .server import mcp_bp

__all__ = ["mcp_bp"]
