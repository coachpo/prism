STATUS: DONE_WITH_CONCERNS

Commits:
- feat!: remove image generations/edits operations

Files changed:
- Removed OpenAI image generation/edit runtime registry entries, hook rows, media wrappers, image adapter bridge, provider image adapter code, route-matrix fixtures, and image-specific tests.
- Shrunk the provider adapter contract by removing `MediaRequest`, `HandleMedia`, and media behavior fields.
- Kept OpenAI Chat Completions, Responses, Responses input_tokens, Responses compact, Anthropic, Gemini, audit capture, pricing, and request-log paths intact.
- Updated runtime/gateway/backend docs and route lists to remove image generations/edits.
- Removed the now-unused runtime media body limit constant.

Tests and commands:
- RED: `cd backend && go test ./internal/httpapi/runtime -run TestResolveRuntimeOperation -count=1` failed because `/v1/images/generations` and `/v1/images/edits` still resolved.
- `cd backend && go test ./internal/httpapi/runtime -count=1` passed.
- `cd backend && go test ./internal/gateway/... -count=1` passed.
- `cd backend && go build ./cmd/prism-backend` passed.
- `git diff --check` passed.
- `rg -in "images\.generations|images\.edits|IsImageOperation|MediaRequest|operationMediaHooks" backend --glob '!docs/**'` returned no matches.
- `cd backend && go test ./tests/runtime -run 'TestRuntimeOperationRouteMatrixSupportedOperations|TestRuntimeOperationNamePersistsForTextAndTokenCount|TestRuntimeOversizedBodiesRejectBeforeProviderAndTelemetry' -count=1` failed before assertions: `docker port prism-s14-runtime-52ee43ad failed: exit status 1` and `no public port '5432/tcp' published`.
- `curl -sS -i -X POST http://192.168.1.222:8088/v1/images/generations --max-time 10` returned HTTP 400 with `Cannot determine model for routing...`, not the expected 404, because that production host is still running the old operation registry.

Concerns:
- The production curl check will not return 404 until this removal is deployed to `192.168.1.222:8088`.
- Docker-backed runtime tests are blocked by the known local Postgres harness port-publish issue before test assertions run.
- The optional gateway/routing image reservation deep-clean was left for a later task; it touches generic IPM/admission route-reason plumbing and is not needed for removing the runtime operations.
