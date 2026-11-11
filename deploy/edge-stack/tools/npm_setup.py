#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
NPM(jc21 Nginx Proxy Manager)自动初始化 + 自动申请 Cloudflare 泛域名证书。
由 setup.sh 调用,全部参数走环境变量;只用标准库,不依赖 pip。

环境变量:
  NPM_URL              如 http://192.168.1.45:81
  NPM_ADMIN_EMAIL      新管理员邮箱(= 登录名)
  NPM_ADMIN_PASSWORD   新管理员密码(留空=跳过改密)
  NPM_ADMIN_NAME       显示名(默认 Administrator)
  CERT_DOMAINS         逗号分隔,如 "*.example.com,example.com"(留空=跳过证书)
  CF_DNS_API_TOKEN     Cloudflare Token(Zone:DNS:Edit)
  LETSENCRYPT_EMAIL    LE 注册邮箱(留空时自动用域名推断)
  PROXY_DOMAIN         要自动创建 Proxy Host 的域名,如 example.com(留空=跳过建反代)
  PROXY_FORWARD_HOST   反代后端地址(默认 127.0.0.1,即 npm-guard 容器自身)
  PROXY_FORWARD_PORT   反代后端端口(默认 81,即 NPM 自己的管理后台)
  PROXY_FORWARD_SCHEME 反代后端协议 http|https(默认 http)
退出码:0 全部 OK 或按配置跳过;非 0 表示有步骤失败(setup.sh 会提示手动完成)。
"""
import json
import os
import sys
import time
import urllib.request
import urllib.error

NPM_URL = os.environ.get("NPM_URL", "").rstrip("/")
EMAIL = os.environ.get("NPM_ADMIN_EMAIL", "").strip()
PASSWORD = os.environ.get("NPM_ADMIN_PASSWORD", "").strip()
NAME = os.environ.get("NPM_ADMIN_NAME", "Administrator").strip() or "Administrator"
CERT_DOMAINS = [d.strip() for d in os.environ.get("CERT_DOMAINS", "").split(",") if d.strip()]
CF_TOKEN = os.environ.get("CF_DNS_API_TOKEN", "").strip()
LE_EMAIL = os.environ.get("LETSENCRYPT_EMAIL", "").strip()
PROXY_DOMAIN = os.environ.get("PROXY_DOMAIN", "").strip()
PROXY_FORWARD_HOST = os.environ.get("PROXY_FORWARD_HOST", "127.0.0.1").strip() or "127.0.0.1"
PROXY_FORWARD_PORT = int(os.environ.get("PROXY_FORWARD_PORT", "81") or "81")
PROXY_FORWARD_SCHEME = os.environ.get("PROXY_FORWARD_SCHEME", "http").strip() or "http"

DEFAULT_EMAIL = "admin@example.com"
DEFAULT_PASS = "changeme"

rc = 0


def log(msg):  print("    [npm] " + msg)
def warn(msg):
    global rc
    rc = 1
    print("    [npm][!] " + msg)


def api(method, path, token=None, body=None, timeout=30):
    url = NPM_URL + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode()
            return resp.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"error": {"message": raw}}
    except Exception as e:
        return 0, {"error": {"message": str(e)}}


def wait_api():
    for _ in range(40):
        st, _ = api("GET", "/api/")
        if st and st < 500:
            return True
        time.sleep(3)
    return False


def login(email, password):
    st, data = api("POST", "/api/tokens", body={"identity": email, "secret": password})
    if st == 200 and isinstance(data, dict) and data.get("token"):
        return data["token"]
    return None


def create_first_admin():
    """新版 NPM(user 表为空、不再自动 seed 默认账号)时,免鉴权创建第一个管理员。
    表非空(已有账号)时该接口会被拒,返回 None 交给上层回退。"""
    if not (EMAIL and PASSWORD):
        log("未设置 NPM_ADMIN_EMAIL/PASSWORD,无法在空库下创建管理员,跳过。")
        return None
    st, data = api("POST", "/api/users", body={
        "name": NAME, "nickname": NAME, "email": EMAIL,
        "roles": ["admin"], "is_disabled": False,
        "auth": {"type": "password", "secret": PASSWORD},
    })
    if st in (200, 201) and isinstance(data, dict) and data.get("id"):
        log("NPM 首次初始化:已创建管理员 %s(id=%s)。" % (EMAIL, data.get("id")))
        return login(EMAIL, PASSWORD)
    return None


def bootstrap():
    """返回登录 token。三种情况:①已是新账号 ②老版默认账号→改成新账号 ③空库→建首个管理员。"""
    # ① 已经是目标账号
    if PASSWORD:
        tok = login(EMAIL, PASSWORD)
        if tok:
            log("NPM 已是目标账号,跳过初始化。")
            return tok
    # ② 老版默认账号 admin@example.com/changeme → 改成新邮箱/密码
    tok = login(DEFAULT_EMAIL, DEFAULT_PASS)
    if tok:
        if not PASSWORD:
            log("未设置 NPM_ADMIN_PASSWORD,用默认账号继续(请手动改密)。")
            return tok
        log("检测到默认账号,更新管理员资料…")
        st, data = api("PUT", "/api/users/1", token=tok, body={
            "name": NAME, "nickname": NAME, "email": EMAIL,
            "roles": ["admin"], "is_disabled": False,
        })
        if st != 200:
            warn("更新用户资料失败:%s" % (data or st))
        st, data = api("PUT", "/api/users/1/auth", token=tok, body={
            "type": "password", "current": DEFAULT_PASS, "secret": PASSWORD,
        })
        if st != 200:
            warn("改密失败:%s" % (data or st))
            return tok
        log("NPM 管理员邮箱/密码已更新。")
        return login(EMAIL, PASSWORD) or tok
    # ③ 空库(新版不自动 seed)→ 免鉴权建首个管理员
    tok = create_first_admin()
    if tok:
        return tok
    warn("NPM 既非目标账号也非默认账号,且无法创建首个管理员(可能已存在未知账号),跳过。")
    return None


def find_cert(token, domains):
    """返回覆盖 domains 中任一域名的证书 id,找不到返回 None。"""
    st, data = api("GET", "/api/nginx/certificates", token=token)
    if st != 200 or not isinstance(data, list):
        return None
    want = set(d.lower() for d in domains)
    for c in data:
        if want & set(d.lower() for d in c.get("domain_names", [])):
            return c.get("id")
    return None


def create_cert(token):
    """申请证书;返回证书 id(已存在则返回现有 id,跳过/失败返回 None)。"""
    if not CERT_DOMAINS or not CF_TOKEN:
        log("未配 CERT_DOMAINS / CF_DNS_API_TOKEN,跳过证书申请。")
        return None
    exist = find_cert(token, CERT_DOMAINS)
    if exist:
        log("已存在覆盖 %s 的证书(id=%s),跳过申请。" % (",".join(CERT_DOMAINS), exist))
        return exist
    email = LE_EMAIL or ("admin@" + CERT_DOMAINS[0].lstrip("*."))
    creds = "dns_cloudflare_api_token = " + CF_TOKEN
    meta_base = {
        "dns_challenge": True,
        "dns_provider": "cloudflare",
        "dns_provider_credentials": creds,
        # Cloudflare DNS 传播等待(秒),给 TXT 记录生效留时间
        "propagation_seconds": 30,
    }
    meta_full = dict(meta_base, letsencrypt_agree=True, letsencrypt_email=email)
    log("申请 Cloudflare 泛域名证书 %s(DNS 挑战,约需 1~2 分钟)…" % ",".join(CERT_DOMAINS))
    for meta in (meta_full, meta_base):   # 先带 email/agree;若 schema 不接受再去掉重试
        st, data = api("POST", "/api/nginx/certificates", token=token, body={
            "provider": "letsencrypt",
            "domain_names": CERT_DOMAINS,
            "meta": meta,
        }, timeout=200)
        if st in (200, 201):
            cid = data.get("id") if isinstance(data, dict) else None
            log("证书申请成功(id=%s)。可在 NPM → SSL Certificates 查看。" % cid)
            return cid
        emsg = ""
        if isinstance(data, dict):
            emsg = (data.get("error") or {}).get("message", "") or json.dumps(data, ensure_ascii=False)
        if "additional properties" in emsg.lower():
            log("该 NPM 版本不接受 letsencrypt_email/agree,去掉后重试…")
            continue
        warn("证书申请失败(HTTP %s):%s" % (st, emsg))
        return None
    warn("证书申请两次尝试均失败,请在 NPM 界面手动申请。")
    return None


def find_proxy_host(token, domain):
    st, data = api("GET", "/api/nginx/proxy-hosts", token=token)
    if st != 200 or not isinstance(data, list):
        return None
    for h in data:
        if domain.lower() in [d.lower() for d in h.get("domain_names", [])]:
            return h
    return None


def create_proxy_host(token, cert_id):
    """为 PROXY_DOMAIN 创建反代(幂等);有证书则绑定并强制 SSL。
    已存在但还没绑证书、而这次拿到了 cert_id,则补绑(覆盖先冒烟后补证书的场景)。"""
    if not PROXY_DOMAIN:
        log("未配 PROXY_DOMAIN,跳过创建反代。")
        return
    exist = find_proxy_host(token, PROXY_DOMAIN)
    if exist:
        hid = exist.get("id")
        if cert_id and not exist.get("certificate_id"):
            log("已存在 %s 的 Proxy Host(id=%s)但未绑证书,补绑证书 id=%s 并强制 SSL…" % (PROXY_DOMAIN, hid, cert_id))
            st, data = api("PUT", "/api/nginx/proxy-hosts/%s" % hid, token=token, body={
                "domain_names": [PROXY_DOMAIN],
                "forward_scheme": PROXY_FORWARD_SCHEME,
                "forward_host": PROXY_FORWARD_HOST,
                "forward_port": PROXY_FORWARD_PORT,
                "access_list_id": exist.get("access_list_id", 0) or 0,
                "certificate_id": cert_id,
                "ssl_forced": True, "http2_support": True,
                "hsts_enabled": False, "hsts_subdomains": False,
                "caching_enabled": exist.get("caching_enabled", False),
                "block_exploits": exist.get("block_exploits", True),
                "allow_websocket_upgrade": exist.get("allow_websocket_upgrade", True),
                "meta": {"letsencrypt_agree": False, "dns_challenge": False},
                "advanced_config": exist.get("advanced_config", ""),
                "locations": exist.get("locations", []) or [],
            })
            if st in (200, 201):
                log("Proxy Host 已补绑证书。")
            else:
                emsg = json.dumps(data, ensure_ascii=False) if isinstance(data, (dict, list)) else str(data)
                warn("Proxy Host 补绑证书失败(HTTP %s):%s" % (st, emsg[:300]))
        else:
            log("已存在 %s 的 Proxy Host(id=%s),跳过创建。" % (PROXY_DOMAIN, hid))
        return
    body = {
        "domain_names": [PROXY_DOMAIN],
        "forward_scheme": PROXY_FORWARD_SCHEME,
        "forward_host": PROXY_FORWARD_HOST,
        "forward_port": PROXY_FORWARD_PORT,
        "access_list_id": 0,
        "certificate_id": cert_id or 0,
        "ssl_forced": bool(cert_id),
        "http2_support": bool(cert_id),
        "hsts_enabled": False,
        "hsts_subdomains": False,
        "caching_enabled": False,
        "block_exploits": True,
        "allow_websocket_upgrade": True,
        "meta": {"letsencrypt_agree": False, "dns_challenge": False},
        "advanced_config": "",
        "locations": [],
    }
    log("创建 Proxy Host:%s → %s://%s:%d%s…" % (
        PROXY_DOMAIN, PROXY_FORWARD_SCHEME, PROXY_FORWARD_HOST, PROXY_FORWARD_PORT,
        "(绑证书,强制SSL)" if cert_id else "(无证书,仅 HTTP)"))
    st, data = api("POST", "/api/nginx/proxy-hosts", token=token, body=body)
    if st in (200, 201):
        log("Proxy Host 创建成功(id=%s)。" % (data.get("id") if isinstance(data, dict) else "?"))
    else:
        emsg = json.dumps(data, ensure_ascii=False) if isinstance(data, (dict, list)) else str(data)
        warn("Proxy Host 创建失败(HTTP %s):%s" % (st, emsg[:300]))


def main():
    if not NPM_URL:
        warn("未设置 NPM_URL,跳过。")
        return rc
    if not wait_api():
        warn("NPM API 未就绪(%s),跳过自动初始化/证书。" % NPM_URL)
        return rc
    token = bootstrap()
    if not token:
        warn("无法取得 NPM token,跳过证书 / 反代。")
        return rc
    cert_id = create_cert(token)
    # 证书刚申请可能还没 ready,补查一次拿 id 给反代绑定
    if cert_id is None and PROXY_DOMAIN and CERT_DOMAINS:
        cert_id = find_cert(token, CERT_DOMAINS)
    create_proxy_host(token, cert_id)
    return rc


if __name__ == "__main__":
    sys.exit(main())
