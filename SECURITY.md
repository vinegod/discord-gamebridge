# Security Policy

## Supported Versions

Only the latest release receives security fixes. Older versions are not patched.

## Scope

The following are considered security vulnerabilities in this project:

- **Command injection** — user input reaching a shell or game console without sanitization
- **Path traversal** — script executor escaping its `allowed_script_dir`
- **Credential exposure** — tokens, passwords, or SSH keys leaked via logs or Discord messages
- **Unauthorized command execution** — permission checks that can be bypassed
- **SSRF / open relay** — bot being used to reach internal network resources

The following are **out of scope**:

- Vulnerabilities in Discord itself or the `disgo` library (report those upstream)
- Issues requiring physical access to the host machine
- Misconfiguration by the operator (e.g., granting `@everyone` permission to destructive commands)

## Reporting a Vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Use GitHub's [private vulnerability reporting](https://github.com/vinegod/discordgamebridge/security/advisories/new) to submit a report. Include:

1. A description of the vulnerability and its impact
2. Steps to reproduce (config snippet, input, or request)
3. Any suggested fix if you have one

You can expect an acknowledgement within **72 hours** and a patch or mitigation plan within **14 days** for confirmed issues.
