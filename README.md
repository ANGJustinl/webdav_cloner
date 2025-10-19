# webdav_cloner | [webdav克隆](docs\README_zh.md)
Clone files between multiple WebDAV endpoints.

## Releases

Pre-built binaries for Linux, Windows, and macOS (both AMD64 and ARM64) are automatically created when a new version tag is pushed to the repository. Download the latest release from the [Releases page](https://github.com/ANGJustinl/webdav_cloner/releases).

To create a new release, simply push a tag starting with `v`:

```bash
git tag v1.0.0
git push origin v1.0.0
```

The GitHub Actions workflow will automatically build binaries for all supported platforms and create a new release with the compiled artifacts.

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
