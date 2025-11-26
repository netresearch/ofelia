# Docker HTTP/2 Support Investigation - Complete Findings

## Executive Summary

Initial investigation revealed HTTP/2 compatibility issues with Unix sockets in v0.11.0. **Deeper investigation revealed the issue affects ALL non-TLS connections**, not just Unix sockets. Docker daemon only supports HTTP/2 over TLS with ALPN negotiation.

## Critical Discovery

### Initial (Incomplete) Understanding
❌ "Unix domain sockets don't support HTTP/2, but network connections do"

### Corrected (Complete) Understanding
✅ "Docker daemon only supports HTTP/2 over TLS (https://), NOT on cleartext connections"

## Docker Daemon HTTP Protocol Support Matrix

| Connection Type | Example | HTTP/2 Support | Reason |
|----------------|---------|----------------|--------|
| **Unix Socket** | `unix:///var/run/docker.sock` | ❌ No | No TLS layer available |
| **Unix Socket** | `/var/run/docker.sock` | ❌ No | Path form, no TLS |
| **TCP Cleartext** | `tcp://localhost:2375` | ❌ No | **No h2c implementation in Docker daemon** |
| **HTTP Cleartext** | `http://localhost:2375` | ❌ No | **No h2c implementation in Docker daemon** |
| **HTTPS with TLS** | `https://host:2376` | ✅ Yes | TLS + ALPN negotiation (RFC 7540) |

## Key Technical Facts

### What is h2c?
- **h2c** = HTTP/2 cleartext (without TLS)
- Defined in RFC 7540 Section 3.1
- Requires special handling and negotiation
- **NOT implemented in Docker daemon**

### Why Docker Daemon Doesn't Support h2c

1. **Connection Hijacking Requirement**
   - Docker uses HTTP/1.1 `Upgrade: tcp` for `docker exec`, `attach`, `logs -f`
   - Hijacking takes over the entire TCP connection
   - HTTP/2 is multiplexed - can't hijack whole connection without breaking other streams
   - RFC 8441 defines WebSocket over HTTP/2, but raw TCP hijacking is HTTP/1.1-specific

2. **Architecture Decision**
   - Moby/Docker Engine relies on Go's `net/http` server
   - No explicit h2c handler registered on cleartext ports
   - Maintains backward compatibility with vast ecosystem of Docker clients

3. **TLS + ALPN for HTTP/2**
   - ALPN (Application-Layer Protocol Negotiation) happens during TLS handshake
   - Client and server negotiate protocol (`h2` for HTTP/2)
   - Without TLS, no ALPN, no HTTP/2

## The v0.11.0 Bug

### What Happened

**v0.11.0 Code**:
```go
transport := &http.Transport{
    ForceAttemptHTTP2: true,  // ❌ ALWAYS true for ALL connections
}
```

**Impact**:
- Client tries to send HTTP/2 preface: `PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n`
- Docker daemon on Unix socket / tcp:// expects HTTP/1.1
- Daemon rejects with protocol error
- ALL non-TLS connections fail

### Affected Connection Types

1. ❌ `unix:///var/run/docker.sock` (default, most common)
2. ❌ `/var/run/docker.sock` (path form)
3. ❌ `tcp://localhost:2375` (cleartext TCP)
4. ❌ `http://localhost:2375` (HTTP cleartext)
5. ✅ `https://host:2376` (would work, but rare in practice)

## The Fix Evolution

### Iteration 1: Initial Fix (Incomplete) ❌

**Logic**:
```go
isUnixSocket := strings.HasPrefix(dockerHost, "unix://") ||
    strings.HasPrefix(dockerHost, "/") ||
    !strings.Contains(dockerHost, "://")

ForceAttemptHTTP2: !isUnixSocket  // ❌ Enables for tcp://, http://!
```

**Problem**: Still enables HTTP/2 for `tcp://` and `http://`, which don't support it!

### Iteration 2: Corrected Fix (Complete) ✅

**Logic**:
```go
isTLSConnection := strings.HasPrefix(dockerHost, "https://")

ForceAttemptHTTP2: isTLSConnection  // ✅ Only for https://
```

**Result**: HTTP/2 only enabled where Docker daemon actually supports it (TLS connections)

## Test Coverage

### Test Matrix

| Connection | DOCKER_HOST Value | Expected HTTP/2 | Test Result |
|-----------|-------------------|-----------------|-------------|
| Unix socket | `unix:///var/run/docker.sock` | Disabled | ✅ Pass |
| Absolute path | `/var/run/docker.sock` | Disabled | ✅ Pass |
| Relative path | `docker.sock` | Disabled | ✅ Pass |
| TCP cleartext | `tcp://localhost:2375` | Disabled | ✅ Pass |
| TCP with IP | `tcp://127.0.0.1:2375` | Disabled | ✅ Pass |
| HTTP cleartext | `http://localhost:2375` | Disabled | ✅ Pass |
| HTTPS with hostname | `https://docker.example.com:2376` | Enabled | ✅ Pass |
| HTTPS with IP | `https://192.168.1.100:2376` | Enabled | ✅ Pass |
| Empty default | `` (empty) | Disabled | ✅ Pass |

**Total: 9 scenarios, all passing**

## Why Initial Fix Was Wrong

### The Mistake

We assumed "not Unix socket" meant "supports HTTP/2". This was based on incomplete understanding of:
1. Docker daemon's protocol capabilities
2. H2c (HTTP/2 cleartext) vs HTTP/2 over TLS
3. Docker's architecture (connection hijacking)

### The Lesson

**Never assume protocol support without verifying**:
- ✅ Research actual implementation (Docker daemon source)
- ✅ Check official documentation
- ✅ Understand protocol negotiation mechanisms (ALPN)
- ✅ Test all connection types, not just the obvious ones

## Technical Deep Dive: HTTP/2 Negotiation

### HTTP/2 over TLS (What Docker Supports)

```
Client                    Server (Docker Daemon)
  |                              |
  |-- TLS ClientHello -------->|
  |   (ALPN: h2, http/1.1)     |
  |                             |
  |<-- TLS ServerHello ---------|
  |    (ALPN selected: h2)      |
  |                             |
  |-- HTTP/2 Connection ------->|
  |   Preface & SETTINGS        |
  |                             |
  |<-- SETTINGS -----------------|
  |   (ACK)                     |
  |                             |
```

**Requirements**:
1. TLS connection established
2. ALPN extension in ClientHello
3. Server selects `h2` via ALPN
4. Both sides use HTTP/2 framing

### HTTP/2 Cleartext (h2c - What Docker Does NOT Support)

```
Client                    Server (Docker Daemon)
  |                              |
  |-- HTTP/1.1 Upgrade -------->|
  |   Upgrade: h2c              |
  |   HTTP2-Settings: ...       |
  |                             |
  |<-- HTTP/1.1 101 ------------|  ❌ Docker daemon doesn't respond
  |   Switching Protocols       |      to Upgrade requests
  |                             |
  |-- HTTP/2 Connection --------|  ❌ Never gets here
  |   (would use HTTP/2)        |
```

**Why Docker doesn't support this**:
- No h2c handler registered on API endpoints
- Connection hijacking breaks h2c model
- Backward compatibility with existing clients

## HTTP/3 Note

**Q**: What about HTTP/3?

**A**: Not supported or relevant:
- HTTP/3 uses QUIC over UDP
- Docker daemon listens on Unix sockets and TCP only
- No UDP listener implemented
- Would require complete re-architecture

## Real-World Deployment Scenarios

### Scenario 1: Local Development (Most Common)
```yaml
# docker-compose.yml
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro
```
**Connection**: Unix socket
**HTTP Version**: HTTP/1.1
**v0.11.0 Bug**: ❌ Broken
**v0.11.1 Fix**: ✅ Works

### Scenario 2: Remote Docker Daemon (Cleartext)
```bash
export DOCKER_HOST=tcp://docker-host:2375
```
**Connection**: TCP cleartext
**HTTP Version**: HTTP/1.1
**v0.11.0 Bug**: ❌ Broken
**v0.11.1 Fix**: ✅ Works

### Scenario 3: Remote Docker Daemon (TLS)
```bash
export DOCKER_HOST=https://docker-host:2376
export DOCKER_CERT_PATH=/path/to/certs
```
**Connection**: HTTPS with TLS
**HTTP Version**: HTTP/2 (via ALPN)
**v0.11.0 Bug**: ❌ Broken (ForceAttemptHTTP2 but no ALPN setup)
**v0.11.1 Fix**: ✅ Works with HTTP/2

## Performance Implications

### HTTP/1.1 Connections (Most Common)
- Connection pooling: ✅ Active
- Request timeouts: ✅ Active
- Circuit breaker: ✅ Active
- HTTP/2 multiplexing: ❌ N/A (not supported by daemon)

### HTTP/2 Connections (Rare: TLS Only)
- All HTTP/1.1 optimizations: ✅ Active
- HTTP/2 multiplexing: ✅ Active
- Header compression (HPACK): ✅ Active
- Server push: ✅ Available (if daemon uses it)

**Key Insight**: Most Docker deployments use Unix sockets (HTTP/1.1), so HTTP/2 optimization is rarely beneficial in practice.

## References

### Official Documentation
- Docker Engine API: https://docs.docker.com/engine/api/
- Docker daemon socket: https://docs.docker.com/engine/security/protect-access/

### RFCs
- RFC 7540: HTTP/2 Specification
  - Section 3.1: Starting HTTP/2 for "http" URIs (h2c)
  - Section 3.3: Starting HTTP/2 with Prior Knowledge
- RFC 7301: ALPN Extension
- RFC 8441: Bootstrapping WebSockets with HTTP/2

### Go Documentation
- net/http Transport: https://pkg.go.dev/net/http#Transport
- ForceAttemptHTTP2: Forces HTTP/2 even without TLS (requires h2c on server)

### Source Code
- Moby (Docker Engine): https://github.com/moby/moby
- go-dockerclient: https://github.com/fsouza/go-dockerclient

## Conclusion

The v0.11.0 bug was more widespread than initially identified:
- **Initial assessment**: Unix sockets broken
- **Actual reality**: ALL non-TLS connections broken

The corrected fix properly identifies that:
- ✅ Only `https://` connections support HTTP/2 (via TLS + ALPN)
- ✅ All other connections are HTTP/1.1 only
- ✅ Docker daemon does not implement h2c (HTTP/2 cleartext)

**Lesson**: Always verify protocol support with actual implementation research, not assumptions based on connection types.
