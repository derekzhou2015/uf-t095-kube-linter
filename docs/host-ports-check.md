# host-ports 检查交付说明

## 1. 背景与缺口

行业基线（CIS Kubernetes Benchmark / Pod Security Standards 同类条文）要求容器不得通过
`hostPort` 在节点网络命名空间暴露端口。审计仓库既有检查后确认：

- 已有 `host-network` / `host-pid` / `host-ipc` 覆盖 Pod 级宿主命名空间字段；
- 已有 `privileged-ports` 检查 **容器内监听端口号**（`containerPort` 是否落在 0–1024）；
- 但没有任何检查读取 `v1.ContainerPort.HostPort`（全仓 grep 仅命中 kubeconform 的 schema
  描述与 OpenShift SCC 夹具），因此违规的 `hostPort` 声明不会被报告。

本次新增独立模板 `host-ports` + 内置检查 `host-ports`，只判定宿主端口暴露，与既有检查的
判定字段和语义均不重叠，故不会冲突或重复报告（已用 `--add-all-built-in` 实跑验证：同一夹具
下 `host-ports` 报告数稳定为 3，未被其它检查复制）。

## 2. 判定对象与语义推导

判定对象是 DeploymentLike 负载内、容器 `ports` 数组中的每一条 `v1.ContainerPort` 声明。

### 2.1 字段缺省语义：`hostPort == 0` 不触发

推导依据：

1. **基线条文**：`hostPort` 是可选项，条文约束的是“把容器端口绑定到节点端口”这一行为；
   未声明该字段即不存在绑定行为。
2. **API 定义先例**（`k8s.io/api/core/v1.ContainerPort`）：
   `HostPort int32 \`json:"hostPort,omitempty"\``，零值即缺省，字段注释写明
   “Number of port to expose on the host … Most containers do not need this.”，
   且合法取值为 `0 < x < 65536`，零不在合法宿主端口范围内。
3. **库内先例**：`privilegedports/template.go` 以 `> 0 && < 1024` 判定容器端口，
   零值天然被排除；本检查沿用同一零值即“未声明”的判定惯例。

结论：仅当 `hostPort > 0` 时产出诊断；字段缺省与显式写 `hostPort: 0` 行为一致，均不触发。

### 2.2 多协议条目语义：逐条判定，协议缺省补 TCP

推导依据：

1. **API 定义**：`Protocol` 注释 “Defaults to \"TCP\"”。
2. **库内先例**：`ports/template.go` 在协议为空字符串时显式补 `"TCP"` 再参与匹配。
   本检查对报告消息采用同一补全规则，保证缺省协议条目与显式 `TCP` 条目输出一致。
3. **逐条判定**：`ports` 是数组，TCP/UDP/SCTP 条目彼此独立；每个非零 `hostPort` 条目
   各自产出一条诊断，消息中带宿主端口号、协议与容器名，输出形态与
   `privileged-ports`（`port %d is mapped in container %q`）保持同构。

## 3. 负载形态普查口径与选型

### 3.1 普查口径：DeploymentLike ObjectKind 的完整 GroupKind 闭包

选型理由：kube-linter 的模板通过 `SupportedObjectKinds` 声明作用域，所有 host-* 家族与
`privileged-ports` 均使用 `objectkinds.DeploymentLike`。该 ObjectKind 的匹配器
（`pkg/objectkinds/deployment_like.go`）枚举了 9 种 GroupKind，这就是模板在对象体系内
**可达的全部负载形态**，以此为普查口径可保证不漏形态，且不把作用域扩大到不携带 PodSpec
的对象：

| # | GroupKind | apiVersion / kind | PodSpec 提取路径 |
|---|-----------|-------------------|------------------|
| 1 | apps/Deployment | apps/v1 Deployment | spec.template |
| 2 | apps/DaemonSet | apps/v1 DaemonSet | spec.template |
| 3 | apps.openshift.io/DeploymentConfig | apps.openshift.io/v1 DeploymentConfig | spec.template（指针） |
| 4 | apps/StatefulSet | apps/v1 StatefulSet | spec.template |
| 5 | apps/ReplicaSet | apps/v1 ReplicaSet | spec.template |
| 6 | /Pod | v1 Pod | spec |
| 7 | /ReplicationController | v1 ReplicationController | spec.template（指针） |
| 8 | batch/Job | batch/v1 Job | spec.template |
| 9 | batch/CronJob | batch/v1 CronJob | spec.jobTemplate.spec.template |

可达性由 `extract.PodSpec`/`PodTemplateSpec` 的类型分支与反射兜底统一覆盖；容器面则经
`customtypes.PodSpec.AllContainers()` 覆盖普通容器、init 容器与 ephemeral 容器
（ephemeral 经 `EphemeralContainerCommon` 字段对齐转换）。因此只判定普通容器会漏掉
init/ephemeral 两类，本检查选用 `util.PerContainerCheck`（基于 `AllContainers`）而非
`PerNonInitContainerCheck`，并有专门夹具锁定。

### 3.2 夹具布局（每类可达形态均有触发与不触发夹具）

- **Go 单测**（`pkg/templates/hostports/template_test.go`，验收主力）：
  - `TestAllDeploymentLikeForms`：9 种形态各建一对夹具——clean（仅 containerPort，
    不触发）与 violating（非零 hostPort，触发），并额外用 registry 的真实 matcher
    断言 9 个 GroupKind 均经 DeploymentLike 可达，防止未来 objectkinds 清单漂移造成漏形态；
  - `TestOmittedAndZeroHostPortDoesNotTrigger`：锁定缺省/显式零不触发；
  - `TestProtocolSemantics`：锁定协议缺省补 TCP、UDP/SCTP 逐条报告；
  - `TestAllContainerClasses`：锁定 init / 普通 / ephemeral 三类容器的覆盖差异。
- **lint 命令夹具**（`tests/checks/host-ports.yml`，第二证据）：与 host-network 等先例
  同构，含 Deployment/DeploymentConfig 的 dont-fire 与触发对象，外加显式零与多协议形态。
  9 形态的完整普查由 Go 单测承担，lint 夹具保持与既有检查一致的精简体例。
- **bats 用例**（`e2etests/bats-tests.sh`）：仓库 `check-bats-tests.sh` 强制内置检查与
  bats 用例一一对应，已同步新增 `host-ports` 用例（Linux CI 专用，不在本次 Windows 验收内）。

## 4. 与既有检查的关系

| 检查 | 判定字段 | 语义 |
|------|----------|------|
| host-network | PodSpec.hostNetwork | 共享宿主网络命名空间 |
| privileged-ports | ContainerPort.containerPort | 容器内特权端口号 |
| **host-ports（新）** | **ContainerPort.hostPort** | **节点端口绑定** |

三者字段不同、可独立触发（例如 hostNetwork=false 仍可声明 hostPort；containerPort=8080
非特权但 hostPort=80 仍暴露节点端口），不存在重复报告。既有检查代码与默认行为零改动，
仅在 `internal/defaultchecks/default_checks.go` 的默认启用集合中追加新检查名
（host-* 家族与 privileged-container 等安全检查同为默认启用）。

## 5. 验收证据

1. `go test ./... -race -count=1`：全量通过（Windows 原生 Go 1.27）。
   其中 `TestCreateContextsWithIgnorePaths` 在改动前的干净基线上即因 POSIX glob/路径分隔符
   在 Windows 失败，已按仓库既有 Windows skip 守卫先例（同文件另外两个测试）补守卫，
   不影响 Linux CI 行为。
2. `golangci-lint run ./...`：零告警退出。
3. lint 命令实跑夹具（第二证据）：
   `kube-linter lint --include host-ports --do-not-auto-add-defaults --format json
   tests/checks/host-ports.yml` → 退出码 1，恰好 3 条报告
   （Deployment TCP 8080、Deployment UDP 5353、DeploymentConfig TCP 8080），
   两个 dont-fire 对象零报告。
