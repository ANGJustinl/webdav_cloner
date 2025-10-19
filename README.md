# webdav_cloner | [webdav克隆](docs\README_zh.md)
Clone files between multiple WebDAV endpoints.

## Build

```powershell
go build -o bin/webdav-cloner ./cmd/webdav-cloner
```

## Configuration

Create a YAML file describing one or more clone jobs. Each job points to a source WebDAV endpoint and one or more targets. Passwords can be provided inline or read from environment variables using `password_env`.

```yaml
jobs:
  - name: mirror-project
    source:
      url: https://source.example.com/remote.php/dav/files/admin
      username: admin
      password_env: SOURCE_PASSWORD
      root: /
    targets:
      - url: https://mirror-one.example.com/webdav
        username: mirror
        password: supersecret
        root: /projects/mirror
      - url: https://mirror-two.example.com/webdav
        username: mirror
        password_env: MIRROR_TWO_PASSWORD
        root: /projects/mirror
    path: project-a
    concurrency: 4
```

`path` is optional and limits the clone to a subdirectory beneath the source root. `concurrency` overrides the global worker count for this job.

## Usage

```bash
webdav-cloner --config config.yaml
```

Flags:

- `--dry-run` prints planned actions without transferring data.
- `--concurrency` overrides the default worker pool size used for file copies.
- `--no-progress` disables the live progress bar (useful when piping output).

The tool retries nothing automatically; rerun the command to continue if a job is interrupted.
