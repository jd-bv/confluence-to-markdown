# confluence-to-markdown
cli to download a confluence space to markdown

## Install
Requires go - https://go.dev/doc/install

```bash
export GOPRIVATE=github.com/jd-bv/confluence-to-markdown
env GIT_TERMINAL_PROMPT=1 go install github.com/jd-bv/confluence-to-markdown@latest
```

## Build

```bash
go build
```

## Usage

```bash
confluence-to-markdown --token <my-personal-confluence-token> --user <my-username@example.com> --baseUrl https://mycompany.atlassian.net SPACEKEY
```
