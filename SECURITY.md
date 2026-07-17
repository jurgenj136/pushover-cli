# Security model

`pushover-cli` is an unofficial, outbound-only client. A send is a
non-idempotent external mutation, so the implementation validates the complete
batch first, sends each target at most once, never retries an ambiguous result,
and reports uncertainty instead of guessing.

## Trust boundaries

- Configuration and aliases are user-owned local metadata. Profile tokens,
  recipient keys, and E2EE keys are secret material.
- Native keychains use the operating-system user account as their protection
  boundary, not the `pushover` process. Another process running as that user can
  read or use those secrets without a new CLI prompt.
- The age-encrypted file backend uses restrictive local permissions and
  authenticated encryption. If unattended operation stores its identity or
  passphrase on disk, a same-user process can use it too; the backend then
  chiefly protects backups, sync copies, and stolen media from off-machine
  disclosure, not against local same-user compromise.
- Environment credentials and `--credentials-stdin` are ephemeral input paths;
  they do not make a secret safe from shell/process inspection or a compromised
  account. The CLI never places their values in argv, routine output, or error
  text.
- Pushover is an external service. HTTPS transport, bounded reads, conservative
  ambiguity classification, and no automatic send retry limit what the client
  can control, but cannot guarantee delivery semantics after bytes leave the
  process.

## Attachments and encryption

Remote attachments require explicit public HTTPS input without URL credentials.
The fetcher rejects loopback, private, link-local, multicast, unspecified, and
otherwise non-public IPv4 and IPv6 destinations before dialing and after every
redirect. Resolved addresses are pinned to prevent DNS rebinding. Redirects,
network phases, response headers, and streamed bytes are bounded independently
of `Content-Length`; content is sniffed and only valid PNG, JPEG, or GIF images
are accepted. Pushover credentials and sensitive headers are never forwarded.
Errors contain only a redacted origin, and the source URL is never sent to
Pushover as attachment metadata. Local attachments are snapshotted into bounded
memory before the first send and are never echoed in routine output.

End-to-end encryption is an explicit, fail-closed compatibility mode. Every
selected recipient must have a valid 64-hex-character key before any target is
sent; there is no plaintext fallback or mixed encrypted/plaintext batch. The
message, title, URL, and URL title are encrypted independently. Attachments,
HTML, and monospace formatting are rejected because they are outside that
encrypted field set.

The official format shares one symmetric key between sender and receiving
devices and uses that same key for AES-256-CBC encryption and HMAC-SHA256
authentication. Fresh randomness prevents deterministic ciphertext, but the
format provides no replay or reordering protection and compression before
encryption leaks approximate plaintext length. It does not protect a message
from a compromised sender, receiving device, shared key, or same-user secret
store. Never commit an E2EE key or test credential.

## Safe operation

Use `--dry-run` to inspect a complete plan, then make a separate explicit send
request. Emergency priority requires explicit retry/expiry values and
confirmation. Do not retry after a failed or ambiguous outcome unless a user
makes a new decision after reviewing the result.

## Reporting a vulnerability

Use GitHub's private Security Advisory workflow for a suspected vulnerability.
Include a minimal reproduction, affected version or commit, and impact. Do not
include live tokens, recipient keys, E2EE keys, message bodies, attachment
contents, or private URLs. There is no public SLA or guaranteed response time;
we will acknowledge and coordinate a fix through the advisory.
