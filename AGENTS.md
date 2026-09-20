# Gou Framework - Project Rules

Reply in Chinese; use Conventional Commits with Chinese or English descriptions (e.g., `feat(process): ...`, `fix(runtime): ...`).

## Overview

Gou is the core application runtime engine and DSL processor for the Yao application engine. It provides the universal process dispatch system, V8 JavaScript/TypeScript execution runtime, DSL-driven data models, HTTP API routers, dataflow orchestration, and connector infrastructure.

- **Ecosystem Role**: Core engine layer imported by `yao`; directly utilizes `kun`, `xun`, and `v8go`.

## Tech Stack

- **Core Runtime**: Go 1.25+ (Toolchain 1.25.5)
- **V8 Engine Binding**: `rogchap.com/v8go` (customized V8 runtime)
- **Database Abstraction**: `github.com/yaoapp/xun` (Capsule, Query, Schema)
- **Foundations**: `github.com/yaoapp/kun` (Logging, Exceptions, Maps, Str)
- **Web & Protocols**: Gin, Gorilla WebSocket, gRPC (`google.golang.org/grpc`), HashiCorp go-plugin
- **Caching & KV**: Go-Redis v8, BuntDB, HashiCorp LRU

## Common Commands & Fast Feedback Loop

```bash
# Fast Feedback Inner Loop (<5s)
go test -v -run TestProcess ./process/...   # Test process registry & execution
go test -v -run TestRunner ./runtime/v8/...  # Test V8 isolate & runner recycling
go test -v -run TestModel ./model/...       # Test model DSL parsing & ORM
make vet                                    # Run go vet on non-example packages

# Quality & Full Suite
make fmt                                    # Format Go files with gofmt -s
make test                                   # Run full test suite across active packages
make bench                                  # Run benchmarks with memory profiling
```

## Navigation & Key Entrypoints

| Module / Component | Primary Entry File / Directory |
| :--- | :--- |
| **Process Dispatch & Registry** | `process/process.go`, `process/register.go` |
| **V8 Isolate Pool & Scope Recycling** | `runtime/v8/runner.go`, `runtime/v8/isolate.go` |
| **CGO Zero-Copy Bridge** | `runtime/v8/bridge/bridge.go` |
| **Model DSL, Relational Mapping & CRUD** | `model/model.go`, `model/types.go` |
| **HTTP API DSL & Guard Routers** | `api/api.go`, `api/http.go` |
| **Query DSL Compiler** | `query/query.go` |
| **DSL Application Loaders** | `application/application.go`, `application/loader/` |

## Directory Structure

```
gou/
├── application/       # Application DSL loaders (file system, bindata, memory)
├── runtime/           # Script runtime engines (v8 isolate pool, runner, bridge, codecache)
├── process/           # Universal process dispatch system, handlers & context propagation
├── model/             # Model DSL, relational mapping, CRUD & schema sync
├── query/             # Gou Query DSL compiler to Xun DBAL queries
├── api/               # HTTP API DSL router, guards, params parser & streaming
├── flow/              # Flow DSL execution engine (DAG and sequential nodes)
├── connector/         # External connectors (database, redis, openai, http)
├── mcp/               # Model Context Protocol server, tools & resource registry
├── plugin/            # HashiCorp go-plugin integration (gRPC/IPC)
├── task/              # Asynchronous background task worker pools
├── schedule/          # Cron-based scheduled job runners
├── websocket/         # WebSocket connection management & broadcast hub
├── store/             # Key-value store abstractions (LRU, Redis, BuntDB)
├── session/           # User session management & storage
└── types/             # Shared interface and structure definitions
```

## System Invariants (DO NOT BREAK)

1. **Single Context Topology per Runner**: Runners in the V8 pool must recycle context scopes via `ctx.ResetRetainedValues()`. Never call `ctx.Close()` and `NewContext()` during normal request execution.
2. **Context-Aware Slow Path**: Slow-Path or long-running processes must cancel immediately when parent `process.Context` cancels; goroutine leakage is strictly forbidden.
3. **Zero-Copy Boundary Views**: When passing binary data between Go and V8, wrap byte slices via `v8go.NewUint8ArrayFromBytes` instead of performing hex/base64 string conversions.

## Footguns & Anti-Patterns (DO NOT)

- **DO NOT** call `ctx.Close()` on pooled runners; use `ctx.ResetRetainedValues()` to avoid destroying warm V8 contexts.
- **DO NOT** register processes after engine boot: all process handlers must be registered during module `Load()` or `init()`.
- **DO NOT** pass non-reusable JSON strings across CGO; prefer raw binary bytes and `Uint8Array`.
- **DO NOT** modify DSL definitions in memory without write-locking `sync.RWMutex`.
- **DO NOT** re-acquire or nest the same `sync.Mutex` / `sync.RWMutex` across callers in the same execution path; Go mutexes are strictly non-reentrant and will cause immediate permanent self-deadlock.
