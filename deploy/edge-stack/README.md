# edge-stack：边缘防护整套链路一键搭建

在一台装好 Docker 的 Linux 主机上一次拉起：

- **CrowdSec**：读反代日志，检测扫描、爆破、CVE 探测。
- **cs-routeros-bouncer**（[jmrplens](https://github.com/jmrplens/cs-routeros-bouncer)）：把 CrowdSec 决策同步到 **MikroTik RouterOS**，在 WAN 入口丢弃。
- **npm-guard**（Nginx Proxy Manager）：用 macvlan 独立 IP 做反代，拿到**真实客户端 IP**。
- 可选 **ipguard** 面板 + 域名访问网关（`--with-ipguard`）。
- 可选 **AdGuard Home**（`--with-adguard`）。

```
访客 ──► RouterOS ──dstnat──► npm-guard(macvlan) ──► 后端服务
            ▲  address-list 丢弃        │ 访问日志
            │                            ▼
   cs-routeros-bouncer ◄── CrowdSec ◄────┘
                              ▲ LAPI
                           ipguard（可选：面板、一键封禁、域名访问网关）
```

## 前提

| 条件 | 说明 |
| --- | --- |
| **MikroTik RouterOS v7** | **必需**，封禁在它的 WAN 入口生效。需开启 **`api`**（8728，bouncer 用）和 **`www`**（REST API，脚本建账号、加端口转发用）服务，且 Docker 主机能访问到。 |
| RouterOS admin 密码 | 首次运行时按提示输入一次，用来建受限账号，不保存。也可写进 `.env` 的 `ROS_ADMIN_PASS` 实现非交互。 |
| Docker + Compose | compose v2 或 v1 均可。 |
| `python3` | 脚本调用 NPM / AdGuard / RouterOS 的 API 时需要（只用标准库）。 |
| 2 个空闲内网 IP | 分别给 npm-guard 和 shim，不能在 DHCP 池里；宿主还需要一张内网网卡作为 macvlan 父接口。 |
| 公网 IP + 域名（可选） | 对外访问时需要。家宽 IP 会变的话自行配置 DDNS（本套不包含）。 |

## 用法

```bash
cp .env.example .env
vi .env                       # 带 ★ 的必改：网卡、网段、两个空闲 IP、RouterOS 地址和 WAN 口
cp overlay/ipguard.env.example overlay/ipguard.env   # 填 ipguard 登录口令
./setup.sh --with-ipguard --with-adguard   # 整套：基线 + ipguard + AdGuard Home（推荐）
```

- **不需要 AdGuard Home？** 去掉 `--with-adguard`（例如宿主 `53` 端口已被占用）。
- **只要基线（不要 ipguard 面板）？** 两个参数都去掉，直接 `./setup.sh`。

脚本是幂等的，可以重复执行；改了 `.env` 重跑一次即可补齐。之后执行 `--verify` 或 `--down` 时，要带上与搭建时相同的 `--with-*` 参数。

```bash
./setup.sh --with-ipguard --with-adguard --verify   # 只跑自检
./setup.sh --with-ipguard --with-adguard --down     # 停服务，并清理 RouterOS 上本栈创建的东西（数据目录保留）
```

### 脚本会做什么

1. 检查网卡和两个 IP，准备目录和 nginx 配置。
2. **RouterOS**：受限账号 `MIKROTIK_USER` 不存在时，用 admin 经 REST API 建一次（权限 `read,write,test,api,rest-api`，**不含 `policy`**，管不了其他用户）。之后所有操作只用这个受限账号。
3. 起 CrowdSec，等待健康后注册 bouncer key，写回 `.env`。
4. 起 bouncer、shim、npm-guard（以及 AdGuard Home / ipguard）。
5. 按 `.env` 自动完成：
   - 改 NPM 默认管理员；
   - 用 Cloudflare DNS 挑战申请证书；
   - 建一个反代域名并绑上证书；
   - 初始化 AdGuard Home（管理员、国内上游 DNS、解析自检）。
6. **RouterOS**：添加端口转发 `PUBLIC_PORT → NPM_IP:443`。规则匹配"目的地址是路由器本身"，外网访问和内网回流都能用（回流需要路由器上已有 hairpin 源地址伪装）。
7. 自检汇总。

### `--down` 会清理什么

停掉本栈容器，删除宿主上的 shim 接口，并经 REST API 用 admin 删除 RouterOS 上本栈创建的内容：端口转发规则、bouncer 建的 raw 规则、`ADDRESS_LIST` 地址表、受限账号及其组。删除按本 `.env` 里的注释、表名、账号名**精确匹配**，不会碰路由器上的其他规则。数据目录（`crowdsec/`、`npm/`、`adguard/`、`ipguard-data/`）保留。

## 常用配置（`.env`）

| 变量 | 作用 |
| --- | --- |
| `PARENT_IFACE` / `LAN_SUBNET` / `LAN_GATEWAY` | macvlan 的父网卡、网段、网关 |
| `NPM_IP` / `SHIM_IP` | 给 npm-guard 和 shim 的两个空闲内网 IP |
| `MIKROTIK_HOST` / `MIKROTIK_USER` / `MIKROTIK_PASS` / `MIKROTIK_WAN_IFACE` | RouterOS API 地址、受限账号、WAN 接口名（raw 丢弃规则只作用于它） |
| `ADDRESS_LIST` | bouncer 写入的 RouterOS 地址表名。和已有的 CrowdSec 部署并存时**必须换成不同的名字**，否则两个 bouncer 会互相覆盖。 |
| `CROWDSEC_DISABLE_CAPI` | `true` 时不拉 CrowdSec 社区黑名单，适合测试或隔离部署，避免上万条地址写进路由器 |
| `PUBLIC_PORT` | 对外端口，会转发到 `NPM_IP:443`。建议用高位端口；部分运营商或公司防火墙会封 `4444` 等常见端口 |
| `NPM_ADMIN_EMAIL` / `NPM_ADMIN_PASSWORD` | NPM 管理员 |
| `CERT_DOMAINS` / `CF_DNS_API_TOKEN` | 证书域名（如 `*.example.com`），以及 Cloudflare Token（权限 `Zone:DNS:Edit`） |
| `PROXY_DOMAIN` / `PROXY_FORWARD_*` | 自动建一个反代，例如 `npm.example.com` → NPM 自己的管理后台 `127.0.0.1:81` |
| `ADGUARD_*` | AdGuard Home 的端口、管理员、上游 DNS。走宿主端口，不占内网 IP；宿主的 `:53` 必须空闲 |
| `IPGUARD_IMAGE` / `REGISTRY_*` | ipguard 镜像。默认用公开镜像，换成私有仓库时填登录信息 |
| `NPM_DB_MODE` | NPM 数据库：`sqlite`（默认）/ `mysql-internal` / `mysql-external`，见下文 |

留空的变量对应的步骤会自动跳过，可以改为在界面里手动完成。

## 数据库（SQLite / MySQL）

| `NPM_DB_MODE` | 行为 |
| --- | --- |
| `sqlite`（默认） | NPM 用内置 SQLite，无需额外数据库 |
| `mysql-internal` | 栈内自带一个 MariaDB；npm-guard 同时挂 macvlan（入站真实 IP）和 bridge（连库） |
| `mysql-external` | 接已有 MySQL，只给 npm-guard 注入 `DB_MYSQL_*`。MySQL 在本宿主上时 `DB_MYSQL_HOST` 填 `SHIM_IP`，在局域网其他机器上时直接填那台机器的 IP |

## 关键点与常见问题

- **真实客户端 IP**：npm-guard 用 macvlan 独立 IP，nginx 直接看到真实源 IP，而不是网关或 Docker 网桥 IP。
- **shim**：macvlan 容器默认访问不到自己的宿主。shim 在宿主上建一个 macvlan 接口打通"宿主 ↔ npm-guard"；反代宿主上的服务时，后端地址用 `host-gw` 或 `SHIM_IP`。
- **封禁落地**：bouncer 把决策写进 RouterOS 地址表，并自建 **raw/prerouting** 丢弃规则。在连接跟踪之前丢弃，省 CPU，一条规则同时覆盖 input 和 forward，只作用于 WAN 口。
- **用域名在内网测试打不开**：内网 DNS 如果把你的域名解析到了别的反代（例如已有的另一套），流量就到不了本栈。可以用手机流量从外网测，或者 `curl --resolve <域名>:443:<NPM_IP> https://<域名>/` 直连验证。
- **证书申请失败（国内）**：NPM 申请时要 `pip install certbot-dns-cloudflare`。可以在 `.env` 打开 `PIP_INDEX_URL` 换成国内 PyPI 镜像，`docker compose up -d npm-guard` 后重跑 `./setup.sh`。
- **IPv6**：默认关闭。
