#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
AdGuard Home 自动初始化(免手动安装向导)+ 解析自检。
由 setup.sh --with-adguard 调用,全部参数走环境变量;只用标准库,不依赖 pip。

环境变量:
  ADGUARD_URL          后台地址,如 http://127.0.0.1:3000
  ADGUARD_USERNAME     管理员用户名(默认 admin)
  ADGUARD_PASSWORD     管理员密码(留空=跳过自动初始化,需手动进向导)
  ADGUARD_WEB_PORT     web 监听端口(默认 3000,与 compose 发布端口一致)
  ADGUARD_DNS_PORT     DNS 监听端口(默认 53)
  ADGUARD_UPSTREAM     上游 DNS(默认 https://dns.alidns.com/dns-query)
  ADGUARD_VERIFY_HOST  自检用宿主可达地址(默认 127.0.0.1),配合 DNS 端口做一次解析
退出码:0 全部 OK 或按配置跳过;非 0 表示有步骤失败(setup.sh 会提示手动完成)。
"""
import json
import os
import socket
import struct
import sys
import time
import urllib.request
import urllib.error

URL = os.environ.get("ADGUARD_URL", "http://127.0.0.1:3000").rstrip("/")
USERNAME = os.environ.get("ADGUARD_USERNAME", "admin").strip() or "admin"
PASSWORD = os.environ.get("ADGUARD_PASSWORD", "").strip()
WEB_PORT = int(os.environ.get("ADGUARD_WEB_PORT", "3000") or "3000")
DNS_PORT = int(os.environ.get("ADGUARD_DNS_PORT", "53") or "53")
UPSTREAM = os.environ.get("ADGUARD_UPSTREAM", "https://dns.alidns.com/dns-query").strip()
VERIFY_HOST = os.environ.get("ADGUARD_VERIFY_HOST", "127.0.0.1").strip() or "127.0.0.1"

rc = 0
SESSION_COOKIE = None


def log(msg):  print("    [adguard] " + msg)
def warn(msg):
    global rc
    rc = 1
    print("    [adguard][!] " + msg)


def api(method, path, body=None, timeout=20):
    url = URL + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    if SESSION_COOKIE:
        req.add_header("Cookie", SESSION_COOKIE)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode()
            return resp.status, (json.loads(raw) if raw and raw.strip().startswith(("{", "[")) else raw)
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw
    except Exception as e:
        return 0, str(e)


def wait_up():
    for _ in range(40):
        st, _ = api("GET", "/")
        if st and st < 500:
            return True
        time.sleep(3)
    return False


def already_configured():
    """/control/status 在"已安装"后可访问(可能 200 或 403 需鉴权);
    未安装时 AdGuard 仍在向导态,configure 可用。"""
    st, data = api("GET", "/control/status")
    if st == 200 and isinstance(data, dict):
        # running 字段存在即已初始化
        return "dns_addresses" in data or "running" in data
    if st in (401, 403):
        return True
    return False


def configure():
    if already_configured():
        log("AdGuard 已初始化,跳过自动安装(如需改密请进后台)。")
        return
    body = {
        "web": {"ip": "0.0.0.0", "port": WEB_PORT},
        "dns": {"ip": "0.0.0.0", "port": DNS_PORT},
        "username": USERNAME,
        "password": PASSWORD,
    }
    log("调用安装接口预置管理员账号 / 监听端口(web=%d dns=%d)…" % (WEB_PORT, DNS_PORT))
    st, data = api("POST", "/control/install/configure", body=body, timeout=40)
    if st in (200, 201):
        log("AdGuard 初始化成功,管理员=%s。" % USERNAME)
        # 初始化后服务会重启一次,等它回来
        time.sleep(5)
        wait_up()
    else:
        msg = data if isinstance(data, str) else json.dumps(data, ensure_ascii=False)
        warn("AdGuard 自动初始化失败(HTTP %s):%s —— 可手动打开后台走向导。" % (st, msg[:300]))


def login():
    """登录拿会话 cookie(初始化后 /control/* 需鉴权)。"""
    global SESSION_COOKIE
    data = json.dumps({"name": USERNAME, "password": PASSWORD}).encode()
    req = urllib.request.Request(URL + "/control/login", data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            sc = resp.headers.get("Set-Cookie", "")
            if sc:
                SESSION_COOKIE = sc.split(";")[0]
                return True
    except Exception:
        pass   # 重启窗口内登录失败属正常,由 set_upstream 重试,最终失败才告警
    return False


def set_upstream():
    """把上游 DNS 设成国内可达(默认装好后是 quad9/cloudflare,国内会超时)。"""
    if not UPSTREAM:
        return
    if not PASSWORD:
        log("未设密码无法登录,跳过设置上游(请手动在后台改上游)。")
        return
    # 初始化后 AdGuard 会重启一次,这里等它回来并重试几次
    st, data = 0, "未尝试"
    for _ in range(6):
        wait_up()
        if login():
            st, data = api("POST", "/control/dns_config", body={
                "upstream_dns": [UPSTREAM],
                "upstream_mode": "load_balance",
            })
            if st in (200, 204):
                break
        time.sleep(3)
    if st in (200, 204):
        log("上游 DNS 已设为 %s。" % UPSTREAM)
    else:
        msg = data if isinstance(data, str) else json.dumps(data, ensure_ascii=False)
        warn("设置上游 DNS 失败(HTTP %s):%s" % (st, str(msg)[:200]))


def dns_query(host, qname="example.com.", timeout=6):
    """直接发一个最小 A 查询到 AdGuard 的 DNS 端口,验证能解析。只用标准库 socket。"""
    tid = 0x1234
    header = struct.pack(">HHHHHH", tid, 0x0100, 1, 0, 0, 0)
    q = b"".join(struct.pack("B", len(p)) + p.encode() for p in qname.rstrip(".").split(".")) + b"\x00"
    packet = header + q + struct.pack(">HH", 1, 1)  # QTYPE=A QCLASS=IN
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.settimeout(timeout)
        s.sendto(packet, (host, DNS_PORT))
        resp, _ = s.recvfrom(512)
        s.close()
        if len(resp) < 12:
            return False, "响应过短"
        ancount = struct.unpack(">H", resp[6:8])[0]
        rcode = resp[3] & 0x0F
        return (rcode == 0 and ancount >= 1), ("rcode=%d ancount=%d" % (rcode, ancount))
    except Exception as e:
        return False, str(e)


def main():
    if not PASSWORD:
        log("未设置 ADGUARD_PASSWORD,跳过自动初始化(请手动打开 %s 走向导)。" % URL)
    else:
        if not wait_up():
            warn("AdGuard 后台未就绪(%s),跳过初始化。" % URL)
            return rc
        configure()
        set_upstream()
    # 解析自检:首次查询会触发上游 DoH 冷启动(bootstrap+TLS 握手)较慢,重试几次
    ok, info = False, ""
    for _ in range(4):
        ok, info = dns_query(VERIFY_HOST)
        if ok:
            break
        time.sleep(3)
    if ok:
        log("DNS 解析自检通过(%s:%d, %s)。" % (VERIFY_HOST, DNS_PORT, info))
    else:
        warn("DNS 解析自检未通过(%s:%d, %s)。可进后台确认上游,或 `nslookup example.com %s` 手测。"
             % (VERIFY_HOST, DNS_PORT, info, VERIFY_HOST))
    return rc


if __name__ == "__main__":
    sys.exit(main())
