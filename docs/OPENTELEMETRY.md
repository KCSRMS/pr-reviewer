# OpenTelemetry in PR Reviewer

This document explains what OpenTelemetry (OTel) is in general, and — more importantly — exactly how it is wired up in **this** codebase: where it's initialized, where spans are created, what data it produces, and where that data goes.

## 1. What OpenTelemetry is

OpenTelemetry is a vendor-neutral standard (API + SDK + wire protocol) for producing **traces**, **metrics**, and **logs** ("telemetry") from an application. The point of it being vendor-neutral is that your code only talks to the OTel API — you can swap the backend that receives the data (Jaeger, Honeycomb, Datadog, Grafana Tempo, a local terminal, etc.) without touching instrumentation code.

The piece this repo uses is **distributed tracing**:

- A **span** represents one unit of work (an HTTP request, a DB query, an LLM call, a job execution). It has a name, a start/end time, key/value **attributes**, and an optional error/status.
- Spans form a **trace** by nesting: a span created with a `context.Context` that already contains a parent span becomes a child of it. This is what lets you see "webhook came in → job enqueued → PR context built → GitHub API called → AI review ran → RAG retrieved 5 docs" as one connected waterfall instead of scattered log lines.
- A **TracerProvider** is the global factory that hands out `Tracer`s, which hand out spans. Where the finished spans go (a network exporter, stdout, or nowhere) is configured once, on that provider, at process startup.

## 2. The SDK setup — `internal/telemetry/tracer.go`

This is the only place the OTel SDK is configured. It's ~60 lines and deliberately minimal:

```go
package telemetry

const serviceName = "pr-reviewer"

func Init(ctx context.Context) (shutdown func(context.Context) error, err error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	var exporter sdktrace.SpanExporter
	if endpoint == "stdout" {
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	} else {
		exporter, err = otlptracehttp.New(ctx)
	}
	if err != nil {
		return nil, err
	}

	res, _ := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func Tracer() trace.Tracer {
	return otel.Tracer(serviceName)
}
```

### Three modes, controlled by one env var

Behavior is entirely driven by `OTEL_EXPORTER_OTLP_ENDPOINT` (declared, commented out, in `.env.example:55`):

| Value | Behavior |
|---|---|
| unset (default) | `otel.SetTracerProvider(noop.NewTracerProvider())` — every `tracer.Start()` call anywhere in the app returns a real-looking but inert span. Zero network calls, negligible CPU overhead. This is the production-safe default if you never configure the var. |
| `"stdout"` | Spans are pretty-printed as JSON to the process's stdout via `stdouttrace`. Useful for local debugging without standing up a collector. |
| any other value (e.g. `http://localhost:4318` or a hosted OTLP endpoint) | Spans are batched and shipped over **OTLP/HTTP** via `otlptracehttp.New(ctx)`, which reads the endpoint from that same env var by OTel convention. |

### Resource and sampling

- `resource.WithAttributes(semconv.ServiceName("pr-reviewer"))` — every span is tagged with `service.name=pr-reviewer`, which is how a backend (Jaeger, Tempo, etc.) distinguishes this service from others sharing the same collector.
- `sdktrace.WithBatcher(exporter)` — spans aren't sent one at a time; they're queued and flushed in batches (the standard `BatchSpanProcessor`), which matters for the OTLP path so review jobs (which can take up to 15 minutes, see `internal/jobs/review_job.go:87-89`) don't hold a live export connection.
- `sdktrace.WithSampler(sdktrace.AlwaysSample())` — no sampling; every trace is captured. Fine at this app's traffic volume (GitHub webhook events), would need revisiting at high volume.

### Shutdown

`main.go` calls `telemetry.Init(ctx)` once at startup (`cmd/server/main.go:58`) and defers the returned `shutdown` func until graceful shutdown, alongside stopping the River queue and HTTP server (`cmd/server/main.go:370-378`):

```go
otelShutdown, err := telemetry.Init(ctx)
...
if err := otelShutdown(shutCtx); err != nil {
	log.Error("otel shutdown error", "error", err)
}
```

`tp.Shutdown` flushes any spans still sitting in the batch processor before the process exits — without this, the last few seconds of spans before a deploy/restart would be silently dropped.

## 3. Instrumentation style: manual, not automatic

There is **no** `otelhttp`, `otelgorm`, `otelsql`, or similar auto-instrumentation library in this repo (`go.mod` only pulls in `otel`, `otel/sdk`, `otel/trace`, and the two exporters). Every span is created by hand with the same three-line pattern:

```go
ctx, span := telemetry.Tracer().Start(ctx, "span.name")
defer span.End()
span.SetAttributes(attribute.String("key", value), ...)
```

This is a deliberate, small surface — spans exist only at the handful of places someone decided were worth tracing, not at every function call.

### Every instrumented span in the codebase

| Span name | File:line | Parent context | Attributes recorded |
|---|---|---|---|
| `webhook.receive` | `internal/http/webhook_handler.go:66` | root (from incoming HTTP request) | `webhook.event` (GitHub event type header) |
| `review.job` | `internal/jobs/review_job.go:93` | root (River worker context — **not** a child of `webhook.receive`, see §4) | `pr.owner`, `pr.repo`, `pr.number` |
| `pr.build_context` | `internal/pr/service.go:29` | child of `review.job` | `pr.owner`, `pr.repo`, `pr.number` |
| `github.post_review` | `internal/github/client.go:142` | child of whichever span called it | `github.owner`, `github.repo`, `github.pr` |
| `ai.review` | `internal/ai/reviewer.go:69` | child of `review.job` | `review.diff_files`, `review.diff_truncated`, `review.rag_enabled`; later augmented (line 221) with `review.comments`, `review.score`, `review.input_tokens`, `review.output_tokens` |
| `rag.retrieve` | `internal/ai/rag/pgvector_retriever.go:26` | child of `ai.review` | `rag.repo_id`, `rag.top_k`; later `rag.docs_returned`; calls `span.RecordError(err)` if the embedding call fails |
| `agent.dispatch` | `internal/ai/agent.go:32` | child of whichever caller passed `ctx` | `agent.name` |

Notice the pattern: attributes are added at span start with what's known immediately (IDs, config flags), and sometimes a second `span.SetAttributes(...)` call later in the function adds results once they're computed (e.g. `ai.review` records token counts only after the LLM call returns).

Error handling is minimal by design — only `rag.retrieve` calls `span.RecordError(err)`. The others return Go errors normally up the call stack; tracing here is for **latency and flow visibility**, not primary error reporting (that's the structured logger's job — see `pkg/logger`).

## 4. How the trace tree actually connects — and where it doesn't

Because every span is created via `telemetry.Tracer().Start(ctx, ...)`, and `ctx` is threaded explicitly through every function signature, parent/child linkage happens automatically wherever `ctx` flows in-process:

```
webhook.receive (HTTP handler)
  └─ (enqueues a River job — trace link ends here, see below)

review.job (River worker, separate goroutine)
  ├─ pr.build_context
  │    └─ (calls into internal/github client for PR + diff — not separately spanned)
  ├─ ai.review
  │    ├─ rag.retrieve
  │    └─ (LLM calls — not separately spanned)
  └─ github.post_review
```

**Important gap:** `webhook.receive` enqueues the review via River (`h.enqueuer.Insert(r.Context(), jobs.ReviewJobArgs{...})` in `internal/http/webhook_handler.go:199`). `ReviewJobArgs` is a plain JSON-serializable struct with no `traceparent`/span-context field, and River's worker later calls `Work(ctx, job)` with a **fresh context** it manages internally — not the original request's context. So `review.job` starts a **brand-new trace**, disconnected from `webhook.receive`. In a backend like Jaeger you'd see two separate traces per PR event, not one connected end-to-end trace from "webhook received" through "review posted." Propagating trace context through the job payload (e.g. injecting `traceparent` into `ReviewJobArgs` and extracting it in `Work()`) would be the fix if end-to-end tracing across the queue boundary is ever needed.

Within `review.job` onward, everything shares one real context and nests correctly.

## 5. This is separate from the Prometheus metrics system

The codebase also has `internal/metrics/metrics.go`, which registers Prometheus metrics (`review_duration_seconds`, `llm_tokens_total`, `review_queue_depth`, `webhook_requests_total`, etc.) via `github.com/prometheus/client_golang/prometheus`. **This is unrelated to OpenTelemetry** — it's a separate, independent telemetry system:

- OTel here = traces only (no OTel metrics or OTel logs pipeline is configured).
- Prometheus = metrics only, scraped separately (not via OTLP).

Don't confuse `metrics.WebhookRequestsTotal.WithLabelValues(...).Inc()` calls (Prometheus counters, seen alongside spans in `webhook_handler.go`) with the tracing calls — they're recorded independently and go to different backends.

## 6. Running it locally

To see traces without any backend, in `.env`:

```
OTEL_EXPORTER_OTLP_ENDPOINT=stdout
```

Every span will be pretty-printed to the server's stdout as it ends.

To send to a real collector (e.g. a local Jaeger or an OTel Collector with an OTLP/HTTP receiver on 4318):

```
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

There is no committed collector config (`otel-collector-config.yaml`) or `docker-compose` entry for a tracing backend in this repo — wiring one up is left to whoever deploys it; the app side only needs that one env var pointed at a valid OTLP/HTTP receiver.

## 7. Suggested improvements

Ranked roughly by impact-for-effort. Each one names the exact file(s) involved.

### High impact, low effort

**7.1 — Mark failed spans with `codes.Error`, not just `RecordError`**
Only `rag.retrieve` (`internal/ai/rag/pgvector_retriever.go:35`) calls `span.RecordError(err)`, and nowhere does the code call `span.SetStatus(codes.Error, ...)`. Without an explicit error status, a trace backend (Jaeger, Tempo, etc.) won't visually flag the span red or let you filter "show me failed traces" — `RecordError` alone just attaches an exception event. Every span that returns a non-nil error should do:
```go
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
    return nil, err
}
```
Cheapest fix in the whole list, and it's the difference between traces being debuggable-by-error vs. just latency waterfalls.

**7.2 — Correlate log lines with trace/span IDs**
`pkg/logger`'s `ExternalCall` helper (and other log call sites) use `slog.InfoContext(ctx, ...)` / `slog.ErrorContext(ctx, ...)` but never pull the trace context out of `ctx`. Right now, given a log line, there's no way to jump to the matching trace, and vice versa. Add a small helper:
```go
func traceAttrs(ctx context.Context) []any {
    sc := trace.SpanContextFromContext(ctx)
    if !sc.IsValid() {
        return nil
    }
    return []any{"trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String()}
}
```
and append it in `ExternalCall` and the handler-level `log.Error(...)` calls. This is the single highest-leverage change for actually *using* the tracing that already exists day to day — most debugging starts from a log line, not a trace UI.

**7.3 — Instrument the LLM calls themselves**
The biggest source of latency and cost in this app — the actual model calls inside `internal/ai/agents/{code_review,security,performance,database,conversation}.go` — has **zero** spans. `ai.review` wraps the whole multi-agent review, and `agent.dispatch` wraps routing to an agent, but the LLM HTTP round-trip inside each agent's `Process()` is invisible. Add a `llm.call` span per agent (provider, model, and — once available — token counts as attributes) inside each agent implementation or in a shared adapter wrapper in `internal/ai/llm/adapters/`. This is what would let you answer "which agent/provider is slow on this PR?" from a trace instead of guessing.

### Medium impact

**7.4 — Use span Links, not a fresh trace, across the River job boundary**
The gap described in §4 (webhook trace and job trace are disconnected) has a correct OTel-native fix that doesn't require forcing a strict parent/child relationship across an async, potentially-much-later-executed boundary: use a **span Link** instead. Inject the webhook span's `SpanContext` into `ReviewJobArgs` as a string (`trace.SpanContextFromContext(ctx).TraceID().String()` + `SpanID().String()`, or just marshal a W3C `traceparent` header via `otel/propagation.TraceContext{}.Inject`), then in `ReviewWorker.Work` reconstruct it and pass it to `tracer.Start(ctx, "review.job", trace.WithLinks(trace.Link{SpanContext: parentSC}))`. Backends that support links (Jaeger, Tempo) will show "caused by" navigation between the two traces without pretending the job ran synchronously inside the HTTP request (it didn't — it can run seconds or minutes later).

**7.5 — Wrap outbound HTTP clients with `otelhttp`**
Neither the GitHub API client (`internal/github/client.go`) nor the LLM provider adapters (`internal/ai/llm/adapters/`) use an instrumented `http.Client`. Swapping in `otelhttp.NewTransport(http.DefaultTransport)` as the client's `Transport` gets you free child spans for every outbound call (with URL, status code, duration) with no per-call-site code changes — much lower effort than manually spanning every GitHub/LLM SDK method, and it would also finally cover `client.GetPullRequest` / `client.GetPullRequestDiff` in `internal/pr/service.go`, which today run *inside* `pr.build_context` but aren't separately visible.

**7.6 — Span the repo-indexing job**
`internal/jobs/index_repo_job.go` has no tracing at all, unlike `review_job.go`. It's the same shape of work (a River job doing GitHub + embedding + DB calls) and would benefit from the same `index.job` root span plus reusing `rag`'s embedding calls' timing.

**7.7 — Richer resource attributes**
`resource.New` (`internal/telemetry/tracer.go:43`) only sets `service.name`. Add `service.version` (git SHA or build tag, likely already available at build time) and `deployment.environment` (from `cfg.AppEnv`, which is already loaded right before `telemetry.Init` in `main.go`). Without these, every span from every environment (dev/staging/prod) looks identical in the backend — you can't filter a trace query down to "prod only."

### Lower priority / future

**7.8 — Configurable sampling**
`sdktrace.AlwaysSample()` is fine at current webhook volume, but is hardcoded. Once volume grows, wire up `sdktrace.ParentBased(sdktrace.TraceIDRatioBased(rate))` with `rate` read from an env var (default `1.0` to preserve today's behavior), so sampling can be turned down without a code change/redeploy.

**7.9 — Decide whether to unify metrics under OTel**
Right now tracing (OTel) and metrics (Prometheus, `internal/metrics/metrics.go`, scraped at `/metrics` per `internal/http/router.go:67`) are two independent systems (see §5). That's a reasonable choice, not a bug — but if the goal is ever a single vendor pipeline (e.g. shipping everything to one SaaS backend via OTLP), the OTel Metrics SDK has a Prometheus-compatible exporter that could replace `client_golang` without changing the metric names/labels already in place.

**7.10 — Local zero-setup trace viewing**
Neither `docker-compose.yml` nor `docker-compose.dev.yml` includes a tracing backend. Adding an `otel-collector` + `jaeger` (all-in-one) service to `docker-compose.dev.yml`, with `OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318` set in the `app` service's env, would let any contributor see real waterfall traces at `localhost:16686` with zero manual setup, instead of the current choice of "stdout dump" or "bring your own external collector."

**7.11 — Regression-proof the instrumentation**
There's no test asserting that a span with a given name/attribute actually gets created. `go.opentelemetry.io/otel/sdk/trace/tracetest` provides an in-memory span recorder — a couple of lightweight tests (e.g. "reviewing a PR produces an `ai.review` span with a `review.score` attribute") would catch instrumentation silently breaking during refactors, which is otherwise invisible until someone notices a gap in production traces.

## 8. Summary of files touched by OTel

```
go.mod                                  — dependency versions (otel v1.37.0, otlptracehttp v1.24.0)
.env.example:55                         — OTEL_EXPORTER_OTLP_ENDPOINT (commented out)
internal/telemetry/tracer.go            — SDK init, exporter selection, Tracer() accessor
cmd/server/main.go:58,377               — Init() call at startup, shutdown() at graceful stop
internal/http/webhook_handler.go:66     — webhook.receive span
internal/jobs/review_job.go:93          — review.job span
internal/pr/service.go:29               — pr.build_context span
internal/github/client.go:142           — github.post_review span
internal/ai/reviewer.go:69              — ai.review span
internal/ai/rag/pgvector_retriever.go:26 — rag.retrieve span
internal/ai/agent.go:32                 — agent.dispatch span
```
