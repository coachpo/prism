# Gateway Core

`pipeline.go` and `envelope.go` define shared request/response contracts; `hooks.go` owns hook permissions and execution records; `routing.go` owns route attempts/reasons; `errors.go` and `classification.go` own shared classifications.

- Hook payload access must be explicit and clone-safe. Declare the minimum body/header read/write permissions and preserve execution records for rejection and transformation paths; `hooks_test.go` exercises this boundary.
- Carry generic operation metadata supplied by runtime. Do not introduce vendor path parsing or a second supported-operation list.
- Keep route reason and selected-target metadata compatible with routing, accounting, and runtime projections when changing shared types.
