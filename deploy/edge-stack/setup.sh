#!/usr/bin/env bash
# ============================================================================
# 家庭边缘防护整套链路 · 一键搭建
#   CrowdSec(检测) + jmrplens cs-routeros-bouncer(落地 RouterOS) + npm-guard(反代/真实IP)
#   可选叠加:ipguard 面板 + 域名级访问网关(--with-ipguard)
#
# 用法:
#   cp .env.example .env && vi .env         # 按本机网络/路由器改参数
#   ./setup.sh                              # 搭基线三件套
#   ./setup.sh --with-ipguard               # 基线 + ipguard 面板/网关
#   ./setup.sh --with-adguard               # 基线 + AdGuard Home(DNS 过滤,走宿主端口)
#   ./setup.sh --verify                     # 只跑自检
#   ./setup.sh --down [--with-ipguard] [--with-adguard]   # 停止(保留数据)
#
# 证书:NPM 的 Cloudflare DNS 挑战泛域名证书在界面里申请(见 README「证书」);
#       国内在 .env 打开 PIP_INDEX_URL 阿里云镜像解锁 certbot-dns-cloudflare 安装。
#
# 幂等:可重复执行。前提:目标机已装 Docker(compose v2 或 v1 均可)。
# ============================================================================
set -euo pipefail
cd "$(dirname "$0")"

log()  { printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[!]\033[0m %s\n' "$*"; }
err()  { printf '\033[31m[x]\033[0m %s\n' "$*" >&2; }
die()  { err "$*"; exit 1; }

if docker compose version >/dev/null 2>&1; then DC="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then DC="docker-compose"
else die "未找到 docker compose(v2)或 docker-compose(v1),请先安装 Docker。"; fi

[ -f .env ] || die "缺少 .env,请先 cp .env.example .env 并按注释修改。"
set -a; . ./.env; set +a

require() { local v; for v in "$@"; do [ -n "${!v:-}" ] || die "环境变量 $v 未设置(见 .env)"; done; }
require TZ LAPI_PORT ADDRESS_LIST PARENT_IFACE LAN_SUBNET LAN_GATEWAY NPM_IP SHIM_IP \
        MIKROTIK_HOST MIKROTIK_USER MIKROTIK_PASS MIKROTIK_WAN_IFACE

# ---- 参数解析(可组合)----
ACTION=up; WITH_IPGUARD=0; WITH_ADGUARD=0
for arg in "$@"; do case "$arg" in
  --down) ACTION=down ;;
  --verify) ACTION=verify ;;
  --with-ipguard) WITH_IPGUARD=1 ;;
  --with-adguard) WITH_ADGUARD=1 ;;
  *) die "未知参数:$arg(可用:--verify / --down / --with-ipguard / --with-adguard)";;
esac; done

# ---- NPM 数据库模式:sqlite(默认)/ mysql-internal(栈内自带)/ mysql-external(接已有)----
COMPOSE_FILES="-f docker-compose.yml"
case "${NPM_DB_MODE:-sqlite}" in
  sqlite) ;;
  mysql-internal) COMPOSE_FILES="$COMPOSE_FILES -f overlay/docker-compose.mysql-internal.yml" ;;
  mysql-external)
    [ -n "${DB_MYSQL_HOST:-}" ] || die "NPM_DB_MODE=mysql-external 需设置 DB_MYSQL_HOST(本宿主上的 MySQL 填 SHIM_IP,别的机器填其 LAN IP)"
    COMPOSE_FILES="$COMPOSE_FILES -f overlay/docker-compose.mysql-external.yml" ;;
  *) die "NPM_DB_MODE 仅支持:sqlite | mysql-internal | mysql-external" ;;
esac
[ "$WITH_ADGUARD" = 1 ] && COMPOSE_FILES="$COMPOSE_FILES -f overlay/docker-compose.adguard.yml"
DC_BASE="$DC $COMPOSE_FILES"
DC_IPG="$DC $COMPOSE_FILES -f overlay/docker-compose.ipguard.yml"

# ---- RouterOS 操作:全走 REST API(www 服务),不用 SSH / 不用 key ----
#   日常(加删 dstnat、卸载清理)用受限账号 MIKROTIK_USER/PASS(无 policy 权限,管不了用户);
#   只有首次建这个受限账号要 admin:读 ROS_ADMIN_PASS,未设则交互提示输入(只用这一次,不落盘)。
#   清理只按本 .env 的 dstnat 注释 / ADDRESS_LIST 精确匹配,碰不到路由器上其它地址表/账号。
ROS_REST_URL="${ROS_REST_URL:-http://${MIKROTIK_HOST%%:*}}"
ros_rest()  { ROS_REST_URL="$ROS_REST_URL" ROS_REST_USER="$MIKROTIK_USER" ROS_REST_PASS="$MIKROTIK_PASS" python3 tools/ros_rest.py "$@"; }
ros_admin() { ROS_REST_URL="$ROS_REST_URL" ROS_REST_USER="${ROS_ADMIN_USER:-admin}" ROS_REST_PASS="${ROS_ADMIN_PASS:-}" python3 tools/ros_rest.py "$@"; }

# 需要 admin 时取密码:优先 .env 的 ROS_ADMIN_PASS,否则交互提示(不回显、不保存)
ros_need_admin() {
  [ -n "${ROS_ADMIN_PASS:-}" ] && return 0
  if [ -t 0 ]; then
    read -r -s -p "请输入 RouterOS ${ROS_ADMIN_USER:-admin} 密码($1,仅本次使用,不保存): " ROS_ADMIN_PASS </dev/tty; echo >&2
    [ -n "$ROS_ADMIN_PASS" ] && return 0
  fi
  warn "未设 ROS_ADMIN_PASS 且无法交互输入"; return 1
}

ros_ensure_account() {
  if ros_rest ping >/dev/null 2>&1; then log "ROS 受限账号 ${MIKROTIK_USER} 可用(REST)"; return 0; fi
  log "ROS 受限账号 ${MIKROTIK_USER} 尚未建立,用 ${ROS_ADMIN_USER:-admin} 建一次"
  ros_need_admin "建受限账号" || return 1
  ros_admin ensure-user "$MIKROTIK_USER" "$MIKROTIK_PASS" || return 1
  unset ROS_ADMIN_PASS
  ros_rest ping
}

verify() {
  log "==== 自检 ===="
  echo "-- 容器状态 --"; $DC_BASE ps
  echo "-- CrowdSec bouncers --"; docker exec crowdsec cscli bouncers list 2>/dev/null || warn "crowdsec 未就绪"
  echo "-- CrowdSec 采集指标 --"; docker exec crowdsec cscli metrics show acquisition 2>/dev/null || true
  echo "-- bouncer 近期日志 --"; docker logs --tail 12 cs-routeros-bouncer 2>&1 | sed 's/^/   /' || true
  [ "$WITH_IPGUARD" = 1 ] && { echo "-- ipguard 日志 --"; docker logs --tail 10 ipguard 2>&1 | sed 's/^/   /' || true; }
  [ "$WITH_ADGUARD" = 1 ] && { echo "-- AdGuard 日志 --"; docker logs --tail 8 adguard 2>&1 | sed 's/^/   /' || true; }
  cat <<EOF

NPM 管理界面:  http://${NPM_IP}:81   (账号 ${NPM_ADMIN_EMAIL:-见 .env})
LAPI:          http://<宿主IP>:${LAPI_PORT}
ROS 地址表:    /ip firewall address-list print count-only where list=${ADDRESS_LIST}
ROS raw 规则:  /ip firewall raw print where comment~"cs-routeros-bouncer"
$([ "$WITH_IPGUARD" = 1 ] && echo "ipguard 面板:   http://<宿主IP>:28090(对外请走 npm-guard 反代)")
$([ "$WITH_ADGUARD" = 1 ] && echo "AdGuard 后台:   http://<宿主IP>:${ADGUARD_WEB_PORT:-3000}   DNS:<宿主IP>:${ADGUARD_DNS_PORT:-53}")
EOF
}

case "$ACTION" in
  down)
    log "停止 edge-stack(保留数据卷)"
    if [ "$WITH_IPGUARD" = 1 ]; then $DC_IPG down; else $DC_BASE down; fi
    # shim 在宿主上建的 macvlan 接口不随容器删除,手动拆掉,释放 SHIM_IP
    ip link del npm_shim 2>/dev/null && log "已删除宿主 npm_shim 接口(释放 ${SHIM_IP})" || true
    # ROS 全量清理(dstnat / raw / 地址表 / 受限账号+组),删账号要 admin,与 up 建账号对称
    log "清理 ROS 上本栈资源(dstnat ${PUBLIC_PORT:-<无>} / raw+地址表 ${ADDRESS_LIST} / 账号 ${MIKROTIK_USER})"
    if ros_need_admin "清理 ROS 并删除受限账号"; then
      ros_admin cleanup "${PUBLIC_PORT:-0}" "$ADDRESS_LIST" || warn "ROS 规则清理失败"
      ros_admin delete-user "$MIKROTIK_USER" || warn "删除受限账号失败"
      unset ROS_ADMIN_PASS
    elif ros_rest ping >/dev/null 2>&1; then
      warn "没有 admin 密码:只用受限账号清理规则,账号 ${MIKROTIK_USER} 保留"
      ros_rest cleanup "${PUBLIC_PORT:-0}" "$ADDRESS_LIST" || warn "ROS 规则清理失败"
    else
      warn "无法连接 ROS,跳过 ROS 清理"
    fi
    exit 0 ;;
  verify) verify; exit 0 ;;
esac

# ---- 预检 ----
log "预检网络参数"
if command -v ip >/dev/null 2>&1; then
  ip -br link show "$PARENT_IFACE" >/dev/null 2>&1 || warn "本机未发现网卡 $PARENT_IFACE,请确认名字(ip -br link)"
  # 本栈已在运行(重跑)时 NPM_IP/SHIM_IP 本就是自己占着的,不再误报
  if [ "$(docker inspect -f '{{.State.Running}}' npm-guard 2>/dev/null)" != "true" ]; then
    for ipaddr in "$NPM_IP" "$SHIM_IP"; do
      ping -c1 -W1 "$ipaddr" >/dev/null 2>&1 && warn "IP $ipaddr 可 ping 通,可能被占用!确认它空闲,否则冲突。" || true
    done
  fi
fi

# ---- 准备目录与配置 ----
log "准备目录与配置文件"
NPM_CUSTOM=npm/data/nginx/custom
mkdir -p crowdsec/config crowdsec/data crowdsec/acquis.d npm/data/logs "$NPM_CUSTOM" npm/letsencrypt
cp -f config/acquis.d/npm.yaml crowdsec/acquis.d/npm.yaml
# 基线 server_proxy.conf(统一 combined 日志 + 监听 4443)
cp -f config/nginx/custom/server_proxy.conf "$NPM_CUSTOM/server_proxy.conf"
: > npm/data/logs/proxy_combined.log

install_ipguard_nginx() {
  log "注入 ipguard nginx 网关(访问宿主 ${SHIM_IP}:28090)"
  cp -f config/nginx/custom/ipguard_http.conf "$NPM_CUSTOM/ipguard_http.conf"
  cp -f config/nginx/custom/ipguard_gate.conf "$NPM_CUSTOM/ipguard_gate.conf"
  sed "s/__IPGUARD_HOST__/${SHIM_IP}/g" config/nginx/custom/ipguard_guard.lua > "$NPM_CUSTOM/ipguard_guard.lua"
  grep -q '_ipguard_auth' "$NPM_CUSTOM/server_proxy.conf" || \
    sed "s/__IPGUARD_HOST__/${SHIM_IP}/g" config/nginx/custom/ipguard_gate_block.conf >> "$NPM_CUSTOM/server_proxy.conf"
  touch "$NPM_CUSTOM/http_top.conf"
  grep -q 'ipguard_http.conf' "$NPM_CUSTOM/http_top.conf" || \
    echo 'include /data/nginx/custom/ipguard_http.conf;' >> "$NPM_CUSTOM/http_top.conf"
}
[ "$WITH_IPGUARD" = 1 ] && install_ipguard_nginx

# ---- RouterOS:确保受限账号(bouncer 写地址表 + 脚本加 dstnat 都用它)----
command -v python3 >/dev/null 2>&1 || die "需要 python3(调用 RouterOS REST API)"
ROS_OK=0
ros_ensure_account && ROS_OK=1 || warn "ROS 受限账号未就绪:封禁不会落地到 ROS,dstnat 也不会自动加"

# ---- 起 CrowdSec 等健康 ----
log "启动 CrowdSec"
$DC_BASE up -d crowdsec
log "等待 CrowdSec LAPI 就绪…"
for i in $(seq 1 40); do
  docker exec crowdsec cscli lapi status >/dev/null 2>&1 && { log "CrowdSec 就绪"; break; }
  sleep 3; [ "$i" = 40 ] && die "CrowdSec 启动超时,查看:docker logs crowdsec"
done

# ---- 注册 bouncer key(幂等)----
if [ -z "${BOUNCER_API_KEY:-}" ]; then
  log "注册 bouncer cs-routeros-bouncer"
  docker exec crowdsec cscli bouncers list -o raw 2>/dev/null | grep -q '^cs-routeros-bouncer,' && \
    docker exec crowdsec cscli bouncers delete cs-routeros-bouncer >/dev/null 2>&1 || true
  KEY="$(docker exec crowdsec cscli bouncers add cs-routeros-bouncer -o raw 2>/dev/null | tr -d '\r\n')"
  [ -n "$KEY" ] || die "注册 bouncer 失败"
  grep -q '^BOUNCER_API_KEY=' .env && { sed -i.bak "s#^BOUNCER_API_KEY=.*#BOUNCER_API_KEY=${KEY}#" .env; rm -f .env.bak; } \
                                   || echo "BOUNCER_API_KEY=${KEY}" >> .env
  export BOUNCER_API_KEY="$KEY"; log "bouncer key 已写入 .env"
else
  log "复用 .env 中已有的 BOUNCER_API_KEY"
fi

# ---- 起基线其余服务 ----
log "启动 bouncer / shim / npm-guard"
$DC_BASE up -d

# ---- 可选:叠加 ipguard ----
if [ "$WITH_IPGUARD" = 1 ]; then
  [ -f overlay/ipguard.env ] || { cp overlay/ipguard.env.example overlay/ipguard.env; warn "已生成 overlay/ipguard.env,请补登录口令后可重跑。"; }
  # 注册 CrowdSec machine 给 ipguard(用 LAPI 增删决策)。
  # 以 CrowdSec 里是否真有该 machine 为准:清空 crowdsec 数据重来后,env 里的旧密码已失效,需重新注册。
  if ! docker exec crowdsec cscli machines list -o raw 2>/dev/null | grep -q '^ipguard,' \
     || ! grep -q '^IPGUARD_CROWDSEC_PASSWORD=..' overlay/ipguard.env; then
    PW="$(openssl rand -hex 16 2>/dev/null || head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    docker exec crowdsec cscli machines add ipguard --password "$PW" --force -f /dev/null >/dev/null 2>&1 \
      || docker exec crowdsec cscli machines add ipguard --password "$PW" --force >/dev/null 2>&1 \
      || die "注册 CrowdSec machine ipguard 失败"
    sed -i.bak "s#^IPGUARD_CROWDSEC_PASSWORD=.*#IPGUARD_CROWDSEC_PASSWORD=${PW}#" overlay/ipguard.env && rm -f overlay/ipguard.env.bak
    log "已注册 CrowdSec machine ipguard 并写入 overlay/ipguard.env"
  else
    log "CrowdSec machine ipguard 已存在,复用"
  fi
  # 私有镜像仓库登录(镜像不公开时需要;公开镜像留空 REGISTRY_TOKEN 即匿名拉取)
  if [ -n "${REGISTRY_TOKEN:-}" ]; then
    printf '%s' "$REGISTRY_TOKEN" | docker login "${REGISTRY_HOST:-ghcr.io}" -u "${REGISTRY_USER:-}" --password-stdin >/dev/null 2>&1 \
      && log "镜像仓库 ${REGISTRY_HOST:-ghcr.io} 登录成功" || warn "镜像仓库登录失败(检查 REGISTRY_USER/REGISTRY_TOKEN)"
  fi
  log "启动 ipguard(镜像 ${IPGUARD_IMAGE:-ghcr.io/duskedge/ipguard:latest})"
  if $DC_IPG up -d ipguard; then
    log "重载 npm-guard 使网关生效"; $DC_BASE restart npm-guard || true
  else
    warn "ipguard 启动失败(原因见上方 docker compose 报错)。基线三件套不受影响;修好后:"
    warn "  $DC_IPG up -d ipguard && $DC_BASE restart npm-guard"
  fi
fi

# ---- 可选:AdGuard Home 自动初始化(免手动向导)+ 解析自检 ----
if [ "$WITH_ADGUARD" = 1 ]; then
  log "等待 AdGuard 就绪并自动初始化(宿主端口 ${ADGUARD_WEB_PORT:-3000})"
  if command -v python3 >/dev/null 2>&1; then
    ADGUARD_URL="http://127.0.0.1:${ADGUARD_WEB_PORT:-3000}" \
    ADGUARD_USERNAME="${ADGUARD_USERNAME:-admin}" ADGUARD_PASSWORD="${ADGUARD_PASSWORD:-}" \
    ADGUARD_WEB_PORT="${ADGUARD_WEB_PORT:-3000}" ADGUARD_DNS_PORT="${ADGUARD_DNS_PORT:-53}" \
    ADGUARD_UPSTREAM="${ADGUARD_UPSTREAM:-https://dns.alidns.com/dns-query}" \
    ADGUARD_VERIFY_HOST="127.0.0.1" \
      python3 tools/adguard_setup.py || warn "AdGuard 自动初始化/自检部分失败,可手动打开后台完成"
  else
    warn "无 python3,跳过 AdGuard 自动初始化,请手动打开 http://<宿主IP>:${ADGUARD_WEB_PORT:-3000} 走向导"
  fi
fi

# ---- NPM 自动初始化(改默认账号)+ 自动申请 Cloudflare 证书 + 建 Proxy Host ----
if command -v python3 >/dev/null 2>&1; then
  log "NPM 自动初始化 / 证书 / 反代(经宿主经 shim 访问 ${NPM_IP}:81)"
  NPM_URL="http://${NPM_IP}:81" \
  NPM_ADMIN_EMAIL="${NPM_ADMIN_EMAIL:-}" NPM_ADMIN_PASSWORD="${NPM_ADMIN_PASSWORD:-}" \
  NPM_ADMIN_NAME="${NPM_ADMIN_NAME:-Administrator}" \
  CERT_DOMAINS="${CERT_DOMAINS:-}" CF_DNS_API_TOKEN="${CF_DNS_API_TOKEN:-}" \
  LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-}" \
  PROXY_DOMAIN="${PROXY_DOMAIN:-}" PROXY_FORWARD_HOST="${PROXY_FORWARD_HOST:-127.0.0.1}" \
  PROXY_FORWARD_PORT="${PROXY_FORWARD_PORT:-81}" PROXY_FORWARD_SCHEME="${PROXY_FORWARD_SCHEME:-http}" \
    python3 tools/npm_setup.py || warn "NPM 自动初始化/证书/反代部分失败,可手动在 http://${NPM_IP}:81 完成"
else
  warn "无 python3,跳过 NPM 自动初始化/证书/反代,请手动在 http://${NPM_IP}:81 完成"
fi

# ---- 路由器公网端口 dstnat(PUBLIC_PORT → NPM_IP:443)----
if [ -n "${PUBLIC_PORT:-}" ]; then
  if [ "$ROS_OK" = 1 ]; then
    log "ROS 配置 dstnat:公网 ${PUBLIC_PORT} → ${NPM_IP}:443"
    ros_rest nat-add "$PUBLIC_PORT" "$NPM_IP" || warn "dstnat 配置失败"
  else
    warn "ROS 受限账号未就绪,跳过 dstnat"
  fi
fi

log "等待 10s 让 bouncer 完成首次对账…"; sleep 10
verify

cat <<EOF

$(printf '\033[32m==== 完成 ====\033[0m')
已自动完成(视 .env 配置):NPM 改默认账号、Cloudflare 泛域名证书、ROS 公网 ${PUBLIC_PORT:-<未设>} → ${NPM_IP}:443 dstnat。
仍需手动:
  1) 登录 http://${NPM_IP}:81(用你在 .env 设的邮箱/密码),加反代(Proxy Hosts)并绑上面的证书。
  2) 域名解析指向家里公网 IP(家宽 IP 会变 → 配 DDNS)。
  3) 若证书自动申请失败(国内多为 pip 装不上):在 .env 打开 PIP_INDEX_URL 阿里云镜像,
     docker compose up -d npm-guard 后重跑 ./setup.sh(幂等,只补证书那步)。
  4) ipguard(若未用 --with-ipguard):见 overlay/ 与 README。

回滚/卸载:./setup.sh --down$([ "$WITH_ADGUARD" = 1 ] && echo " --with-adguard")$([ "$WITH_IPGUARD" = 1 ] && echo " --with-ipguard")(停服务并清理 ROS 上本栈资源,数据目录保留)
EOF
