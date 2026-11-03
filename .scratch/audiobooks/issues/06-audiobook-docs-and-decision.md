# Docs, decision status and release notes

Status: resolved
Blocked by: 05

- Move D031 from Proposed to Accepted once 01-05 merge; add amendments for anything that changed.
- Update `docs/V1_SCOPE.md`, `docs/IMPLEMENTATION_PLAN.md`, `docs/research/PROJECT_STATUS.md`, `AGENTS.md` media-set wording and the OpenAPI changelog.
- Document the third-party dependency: Audnexus/Audible are unofficial; AudiobookDB's key tier forbids long-term caching, so it is not used in v1 (research §3).
- Record remaining open questions from the research file as follow-ups: series/box-set ASINs, `updatedAt` skipping, Amazon URLs.

Acceptance: docs match shipped behaviour and `scripts/check.sh` passes.

## Answer

D031 is Accepted with the amendments above and the follow-ups listed. Updated V1_SCOPE, AGENTS.md (entry and artwork rules), GLOSSARY, DOMAIN_MODEL, PROJECT_STATUS, IMPLEMENTATION_PLAN and INTEGRATIONS (new Audible section naming the unofficial dependency and why AudiobookDB is not used). `api/openapi.yaml` has no changelog section, so there was nothing to add there; its enums and descriptions were updated with the code.
