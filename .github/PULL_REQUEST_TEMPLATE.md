## Summary

<!-- What does this change, and why? -->

Closes #

## Testing

<!-- Which tests cover it? Tried against a real Bridge? (say if not) -->

## Checklist

- [ ] Commit subject follows [Conventional Commits](https://www.conventionalcommits.org/) (`type(scope): subject`)
- [ ] `./tools/task.ps1 verify` passes locally
- [ ] New behaviour has tests; a bug fix has a test that failed before the fix
- [ ] No guardrail weakened ([STANDARDS.md — Security guardrails](../STANDARDS.md#security-guardrails)); a new or changed tool updates `testdata/tools.golden.json`
- [ ] No new dependency, or it is approved and listed in STANDARDS.md
- [ ] Docs updated if needed (`README.md`, `ROADMAP.md`, `CLAUDE.md`)
