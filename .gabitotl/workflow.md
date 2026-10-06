---
output: compact
language: es-AR
persona: gabito
backup_before_write: true
memory_before_work: true
memory_after_work: true
backup:
  max_snapshots: 20
  max_age_days: 7
context_limit_tokens: 200000
context_threshold: 0.75
branding_guard: strip
branding_extra_terms: []
# Only the detector corpus may contain branding strings on purpose. Everything
# else (tests, docs) builds them at runtime or describes them without quoting.
branding_allow_paths:
  - internal/branding/testdata/
guard_hooks: ask
---

# Workflow

GabitoTL dogfoods its own authorship guard: `gabitotl guard scan --all` must
report zero findings on this repository.
