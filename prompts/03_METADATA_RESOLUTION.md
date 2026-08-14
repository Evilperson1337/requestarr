# Prompt 03 — Metadata URL Resolution

Implement the resolver registry and provider adapters for TMDB movie/TV URLs, MusicBrainz release-group/recording URLs, and IGDB game URLs. Resolver output must be provider-neutral `domain.ResolvedItem`. Add bounded HTTP clients, caching, rate-limit awareness, error taxonomy, sanitized fixtures, and tests for malformed/unsupported URLs. IGDB must return the game's actual supported platforms for dynamic request controls. Do not add global search/discovery.
