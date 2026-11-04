# 07: The Unraid guide and the manual checklist

**What to build:** A reader with an Unraid server and no prior knowledge of Requestarr can install it, sign in as the first admin and connect a library by following one document. A manual checklist turns "it works on Unraid" into something repeatable.

**Blocked by:** 01, 04, 06 (it documents their behaviour and fields)

**Status:** ready-for-agent

- [ ] The guide opens with what the user must bring (PostgreSQL, a TLS reverse proxy, an HTTPS OIDC provider) and says plainly that none of them is bundled and why https is required for the public URL and issuer
- [ ] It shows how to create the database and user in an existing PostgreSQL container and build the database URL
- [ ] It gives a worked Keycloak client (confidential client, exact redirect URI, standard scopes) and the generic OIDC requirements for other providers
- [ ] It explains both install paths (template and compose), the template fields, and generating the two secrets, with the master key backup warning
- [ ] It shows how to get the one-time first-admin token from the running container, claim the admin after sign-in, and that new users sign in as viewers
- [ ] It explains that a library address on a private host may use `http://`, that its token then travels in cleartext on the network, the extra-networks setting, and that container names resolve only on a user-defined Docker network
- [ ] It links to the upgrade and backup guide and the release documentation, and is linked from the README
- [ ] The manual testing guide gains a real-server checklist: first-admin sign-in through Keycloak, a healthy container, a library syncing over a private `http://` address, installing from the Template Repositories URL
- [ ] `scripts/check.sh` passes
