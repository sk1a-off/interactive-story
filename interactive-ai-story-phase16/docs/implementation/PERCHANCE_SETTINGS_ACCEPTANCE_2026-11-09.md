# Perchance settings acceptance — 2026-11-09

Stage 1 implementation is complete. Automated checks pass. Live browser and
Perchance generation acceptance remains open.

## Implemented behavior

- New migration `00036_perchance_image_dimensions.sql` permits 768×768,
  512×768, and 768×512 with 1–4 variants. Existing records remain valid.
- Down restores the old square/two-variant restrictions and fails with an
  explicit explanation if incompatible generations or variants exist.
- AI revisions store `image_resolution` and `image_count`. Missing legacy
  settings resolve to 768×768 and two variants. Explicit counts outside 1–4
  are rejected.
- Every image creation path pins width, height, count, and configuration
  revision. Stored metadata uses the real dimensions. Historical provider
  profile strings are retained; they do not determine output parameters.
- Application and repository completion validate the pinned count. Duplicate
  variant numbers are rejected and successful completion remains idempotent.
- The worker API sends actual dimensions/count; the runtime generates the
  requested number of images. Legacy jobs without `count` produce two.
- AI settings expose the two output controls. Reader shows 1–4 images with
  their natural proportions and matching pending placeholders. One image
  occupies the full row and has no selection controls.
- Reader and settings show worker connection state, current output dimensions
  and count when drawing, and a link to open the worker tab. Stale heartbeats
  do not present an old job as still active.
- Public worker status is read-only. Configured worker-token protection
  continues to cover every internal worker endpoint.

Style, negative prompts, guidance, seed handling, and automatic illustration
scheduling policy were not changed by this stage.

## Executed checks

| Check | Result |
| --- | --- |
| Go format, vet, full tests, race | PASS |
| PostgreSQL integration suite with race | PASS |
| Clean database migrations 00001–00036 | PASS |
| Goose 00036 down, then up | PASS |
| Actual migration SQL with legacy rows and incompatible data | PASS, using transaction-local temporary tables |
| Allowed resolution/count combinations and invalid values | PASS |
| Immutable generation settings and persisted metadata | PASS |
| Completion with 1 and 4 images, wrong count, duplicate variants, replay | PASS |
| Reader API exposes pinned dimensions/count | PASS |
| Frontend clean install, typecheck, lint, build | PASS |
| Frontend tests | 41/41 PASS |

PostgreSQL validation used the separate local database
`story_image_settings_00036_check`; application data was not migrated or edited
for this acceptance run. Runtime JavaScript tests use a simulated DOM and a
stubbed image provider, not the real Perchance plugin. Component tests validate
markup and behavior, not rendered browser layout.

## Target-machine acceptance still required

1. Apply migration 00036 to the application database and restart/rebuild the
   application from this checkout.
2. Update both the installed Violentmonkey script and Perchance runtime HTML.
   Configure the worker token in the userscript menu if the backend requires it.
3. In AI settings save/activate portrait output with one variant; create a new
   illustration and verify the full uncropped frame in Reader.
4. Save/activate landscape output with four variants; verify four returned
   images and unchanged dimensions/count on the earlier generation.
5. Close/reopen the worker tab and verify offline/online/drawing transitions.
6. Inspect Reader and settings on desktop and a narrow mobile viewport.

No real Story LLM/Perchance latency or image quality measurements are claimed.
