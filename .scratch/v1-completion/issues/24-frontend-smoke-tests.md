# 24: Frontend smoke tests

**What to build:** The main pages render and behave against a mocked API.

**Blocked by:** 18 Requests page controls, 22 Wizard Notifications, Request Policy, Review and Finish, 23 Operational status view

**Status:** ready-for-agent

- [ ] Requests, request modal, notifications, Settings and the wizard each have a smoke test
- [ ] A frontend test command runs in make web-check or a sibling target and in CI
- [ ] No third-party runtime dependency is added for the app itself
