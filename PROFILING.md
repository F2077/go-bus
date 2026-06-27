# Profiling

CPU and allocation profiles for the `bus` package, produced by running the
benchmark suite with `-cpuprofile` / `-memprofile`. Artifacts land in
`.profile/` (gitignored).

## Targets

| Command | Produces | Answers |
|---|---|---|
| `make profile-cpu` | `.profile/cpu.prof` | where is time spent per Emit / On? |
| `make profile-mem` | `.profile/mem.prof` | where do allocations come from? |

Both run every benchmark in `./bus/...` for 2s. To focus on one benchmark,
append `-run='^$' -bench=BenchmarkName` to the underlying `go test` command.

## Reading a profile

	go tool pprof -http=:8080 .profile/cpu.prof

Then open the browser. Useful views: **FLAME** (flame graph of cumulative cost),
**TOP** (hottest functions). For allocations, repeat with `mem.prof`.

## Workflow: validate a fix

1. `make profile-mem` on `main` → baseline.
2. Apply the change.
3. `make profile-mem` again → compare in pprof.

The emit path is expected to stay at **0 allocs/op** (see `BenchmarkEventBus`);
any regression surfaces immediately in `mem.prof`.
