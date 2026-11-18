# Customer login and password: design

## Intent
Customers create their own account and sign in. Every customer-scoped route (locations, invoices, estimates, balance, bookings, payment methods) must act for the signed-in customer only. Before this, those routes trusted an `X-Customer-ID` header.

## Approaches considered
- **A. Opaque session tokens stored hashed in Postgres (chosen).** Revocable instantly, one indexed lookup per request, only new dependency is `golang.org/x/crypto/bcrypt`.
- B. Short JWT + refresh token: no per-request DB hit, but needs a library, a signing secret and a refresh table to revoke anything. Not worth it at this scale.
- C. External identity provider: free reset/MFA, but a vendor and an integration that does not fit this hand-written stack.

## Design
- `POST /api/v1/auth/register` (name, email, password, optional phone) creates the customer and a session. `POST /api/v1/auth/login` returns a session. Both are public and rate limited (10 per minute per IP).
- `POST /api/v1/auth/logout` and `POST /api/v1/auth/password` need a session. Changing the password ends every other session and keeps the current one.
- `Authorization: Bearer <token>`: 32 random bytes, base64url; only the SHA-256 is stored in `sessions`. Lifetime is `SESSION_TTL` (default 720h), no sliding refresh.
- Passwords: bcrypt cost 12, 10 to 72 bytes. A failed login is always `401 invalid credentials`; an unknown email still costs one bcrypt compare, so neither the body nor the timing reveals which emails exist.
- Emails are unique ignoring case (`lower(email)` unique index), which also applies to staff-created customers.
- `RequireSession` replaces `RequireCustomer` on customer routes. Staff routes (document writes) keep `X-Admin-Key` + `X-Customer-ID`. `middleware.CustomerID(c)` is unchanged, so no feature code changed.

## Goals / non-goals
- Goals: real per-customer authentication; revocation; no change to feature handlers.
- Non-goals: email verification at sign-up, a dedicated invite flow for staff-created customers, MFA, admin accounts. (Password reset was added later, see below.)
- Anti-goal: letting someone claim an existing staff-created customer by registering its email. Registration with such an email is a 409; those customers get in through password reset instead.

## Known limits (also marked `shortcut:` in code)
- Register answers 409 for a taken email, which reveals that the address is registered. Fix with email verification.
- The rate limiter is in memory and per process, and trusts `X-Forwarded-For` until trusted proxies are configured.
- Expired sessions are purged per customer at login; there is no global sweeper.
- Migration 000011 fails if two existing customers share an email ignoring case; fix the data first.

## Password reset (added later)
- `POST /api/v1/auth/password-reset` {email} always answers 202. If the email belongs to a customer, a token (32 random bytes, only its SHA-256 stored in `password_resets`) is emailed as `RESET_URL_BASE?token=...`. It lasts 1 hour, works once, and a newer request replaces the older link.
- `POST /api/v1/auth/password-reset/confirm` {token, new_password} sets the password and ends all of that customer's sessions in one SQL statement; a bad, spent or expired token is 400. Both endpoints share the login/register rate limit.
- Sending goes through `service.Mailer`. With `MAIL_API_KEY` set (and `MAIL_FROM`) mail goes out through the Resend HTTP API; otherwise the message, link included, is written to the log.
- A staff-created customer with an email can set a first password this way: reading the email proves the address is theirs, so this is safe where claiming by registration is not.
- Known limits: the email is sent inline, so response time still differs for registered addresses; a mail failure is logged and hidden from the caller; the 1 hour lifetime is a constant; real Resend delivery was not run against the live API.
