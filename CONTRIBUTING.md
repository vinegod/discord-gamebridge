# Contributing to Discord Gamebridge

## Development Setup
### Fork and Clone

1. Fork the repository and clone it to your local machine.

```bash
git clone https://github.com/yourusername/discordgamebridge.git
cd discordgamebridge
```

2. Install [Go](https://go.dev/)
    Ensure you have [Go](https://go.dev/) version 1.25.0 or higher installed.

3. Install [pre-commit](https://pre-commit.com/) (Required)
This project uses [pre-commit](https://pre-commit.com/) to enforce code quality, format Go code, lint shell scripts, and prevent accidental secret commits (via Gitleaks). You must install it before committing any code.

To manually run all checks against your current files before committing:
```bash
pre-commit run --all-files
```

## Contribution Rules
1. Code Quality and Linting

- All code must pass the pre-commit hooks. If a hook fails, the commit will be rejected.
- The project uses golangci-lint with strict complexity and formatting rules configured in .golangci.yaml. Do not bypass the linter.
- If a linter rule must be ignored for a specific line, use the //nolint:<linter_name> // reason: <explanation> directive. You must provide a valid reason.

2. Security

    Never commit secrets. The repository handles Discord tokens and webhooks. The gitleaks hook is active, but always double-check your `.env` or `config.yaml` files are excluded from commits. `.env` and `config.yaml` are listed in .gitignore.

3. Bash Scripts

- Any additions or modifications to shell scripts (e.g., inside scripts/) must pass shellcheck. Use standard Bash, avoid Bashisms where possible, and quote your variables.

4. Commit Messages

- Write concise and descriptive commit messages.
- [Semantic Commit Messages](https://gist.github.com/joshbuchea/6f47e86d2510bce28f8e7f42ae84c716) (thanks for preparing this gist)
    Format: `<type>(<scope>): <subject>`
    `<scope>` is optional

    **Example**

    ```
    feat: add hat wobble
    ^--^  ^------------^
    |     |
    |     +-> Summary in present tense.
    |
    +-------> Type: chore, docs, feat, fix, refactor, style, or test.
    ```

    - `feat`: (new feature for the user, not a new feature for build script)
    - `fix`: (bug fix for the user, not a fix to a build script)
    - `docs`: (changes to the documentation)
    - `style`: (formatting, missing semi colons, etc; no production code change)
    - `refactor`: (refactoring production code, eg. renaming a variable)
    - `test`: (adding missing tests, refactoring tests; no production code change)
    - `chore`: (updating grunt tasks etc; no production code change)

5. Pull Requests
    - Create a feature branch from main (e.g., feature/docker-support or fix/tailer-crash).
    - Keep pull requests focused on a single issue or feature.
- Verify that your code builds successfully via `go build` and that `pre-commit run --all-files` passes locally before pushing.

## ToDo
- [ ] Prepare MAKEFILE with commands to install all build dependencies
