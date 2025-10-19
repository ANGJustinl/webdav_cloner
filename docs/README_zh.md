# WebDAV 文件克隆工具

`webdav-cloner` 是一个使用 Go 编写的命令行工具，可在多个 WebDAV 端点之间同步（克隆）文件。工具会遍历源端目录，将新增或更新的文件复制到一个或多个目标端，可配合 `--dry-run` 预览操作结果。

## 快速开始

1. 安装 Go 1.21 及以上版本。
2. 克隆或下载本仓库，并在项目根目录执行：
   ```powershell
   go build -o bin/webdav-cloner ./cmd/webdav-cloner
   ```
3. 将根目录中的 `config.template.yaml` 复制为 `config.yaml`，并根据实际环境修改内容。
4. 运行工具：
   ```powershell
   .\bin\webdav-cloner --config config.yaml
   ```

## 配置文件说明

配置文件采用 YAML 格式，支持配置多个任务（`jobs`）。每个任务包含一个源端（`source`）和一个或多个目标端（`targets`）。你可以直接在 `config.template.yaml` 中修改，也可以参考以下字段说明手动编写：

- **jobs**：任务列表。每个条目表示一次克隆任务。
- **job.name**：任务名称，用于日志输出。
- **job.source**：源端 WebDAV 信息。
  - `url`：WebDAV 根地址。
  - `username` / `password`：认证信息。
  - `password_env`：可选，指定从环境变量读取密码（若 `password` 留空则必须设置）。
  - `root`：同步起始目录，默认为 `/`。
- **job.targets**：目标端列表，每个目标结构与 `source` 类似。
- **job.path**：可选，仅同步源端特定子目录，例如 `project-a`。
- **job.concurrency**：可选，覆盖全局并发数，为每个任务设置独立的拷贝 worker 数量。

配置示例（节选自模板）：

```yaml
jobs:
  - name: "示例任务"
    source:
      url: "https://source.example.com/remote.php/dav/files/admin"
      username: "admin"
      password_env: "SOURCE_PASSWORD"
      root: "/"
    targets:
      - url: "https://mirror-one.example.com/webdav"
        username: "mirror"
        password_env: "MIRROR_ONE_PASSWORD"
        root: "/projects/mirror"
    path: "project-a"
    concurrency: 4
```

## 运行与常用参数

```powershell
webdav-cloner --config config.yaml [--dry-run] [--concurrency 8] [--no-progress]
```

- `--config`：必选，指定配置文件路径。
- `--dry-run`：仅打印计划动作，不执行真实写入，常用于确认配置是否正确。
- `--concurrency`：设置默认拷贝并发数（每个任务可在配置文件中覆写）。
- `--no-progress`：关闭实时进度条，适用于将输出重定向到文件时。

执行过程中可使用 `Ctrl + C` 中断；下次重试时工具会跳过已存在、时间戳与大小一致的文件。

## 故障排查与建议

- 若提示认证失败，检查用户名、密码或环境变量是否正确。
- 如果需要为大量文件加速同步，可提升 `--concurrency` 或任务级 `concurrency` 的值，但建议逐步调试以避免压垮目标服务器。
- 使用 `--dry-run` 验证路径、目标目录权限以及任务范围，确认无误后再执行真实同步。
- 默认会显示同步进度条；若终端不支持或希望保持输出纯文本，请添加 `--no-progress`。
- 日志输出中会展示跳过的文件（已最新）与实际复制的文件，便于比对同步结果。

如需英文文档，请参阅项目根目录中的 `README.md`。
