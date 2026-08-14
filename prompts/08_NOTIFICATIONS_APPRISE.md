# Prompt 08 — Notifications + Apprise

Implement event-driven in-app notifications and Apprise API delivery. State changes fan out only to request subscribers and relevant administrators. Persist deliveries/attempts, bounded retry policy, suppression/deduplication, and test notification in Settings/setup. Implement against the current upstream Apprise API contract and document supported configuration mode. Never log target URLs/credentials when they contain secrets.
