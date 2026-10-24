# 20: Wizard draft and first steps

**What to build:** The setup wizard keeps a resumable draft and handles General, Authentication and Metadata Providers with connection checks. It states exactly which secrets still need an environment change (D012, D017).

**Blocked by:** 06 Consistent error shape and request ID, 11 Adapter interface, contract suite, multi-source availability and Plex movie/TV

**Status:** done

- [x] Draft state is persisted and a later session resumes at the right step
- [x] Each step validates its external connection and reports clear errors
- [x] Only non-secret settings and the names of secret environment variables are stored
- [x] A panel lists what still needs setting in the environment and a restart
- [x] Tests cover resume, validation failure and the secrets rule
