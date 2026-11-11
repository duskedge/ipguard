#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
RouterOS v7 REST API 小助手(只用标准库 + http,甩掉 SSH/key)。
由 setup.sh 调用,给 ROS 建受限账号 / 加删 dstnat / 清理本栈资源。

环境变量:
  ROS_REST_URL    如 http://192.168.1.1(REST over www 服务)
  ROS_REST_USER   REST 登录用户
  ROS_REST_PASS   REST 登录密码
动作(argv[1]):
  ping                       连通+鉴权自检
  ensure-user USER PASS      建受限用户+同名组(需 admin 凭据);幂等
  delete-user USER           删受限用户+同名组(需 admin 凭据);不存在则跳过
  nat-add PORT TOADDR        加 dstnat(先按注释删同名再加,外网+内网回流都可用),注释固定 "edge-stack npm PORT"
  nat-del PORT               删注释为 "edge-stack npm PORT" 的 dstnat
  cleanup PORT LIST          删 dstnat + 删 raw(src-address-list=LIST)+ 删 address-list(LIST)
退出码:0 成功;非 0 失败(setup.sh 据此提示)。
"""
import base64
import json
import os
import sys
import urllib.parse
import urllib.request
import urllib.error

URL = os.environ.get("ROS_REST_URL", "").rstrip("/")
USER = os.environ.get("ROS_REST_USER", "")
PASS = os.environ.get("ROS_REST_PASS", "")

GROUP_POLICY = "read,write,test,api,rest-api,!policy,!ftp,!reboot,!password,!sniff,!sensitive,!romon"


def _req(method, path, body=None, timeout=12):
    url = URL + "/rest" + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    tok = base64.b64encode(("%s:%s" % (USER, PASS)).encode()).decode()
    req.add_header("Authorization", "Basic " + tok)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode()
            return resp.status, (json.loads(raw) if raw.strip() else None)
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"message": raw}
    except Exception as e:
        return 0, {"message": str(e)}


def die(msg):
    print("    [ros] " + msg, file=sys.stderr)
    sys.exit(1)


def log(msg):
    print("    [ros] " + msg)


def ping():
    st, data = _req("GET", "/system/identity")
    if st == 200:
        name = data.get("name") if isinstance(data, dict) else "?"
        log("REST 连通 OK(identity=%s)。" % name)
        return
    die("REST 连通/鉴权失败(HTTP %s):%s" % (st, data))


def find(path, **q):
    # 查询值必须 URL 编码,且空格要编成 %20(ROS REST 不认 "+"):注释 "edge-stack npm 4444" 带空格
    qs = urllib.parse.urlencode(q, quote_via=urllib.parse.quote)
    st, data = _req("GET", path + ("?" + qs if qs else ""))
    if st != 200 or not isinstance(data, list):
        return []
    return data


def delete_where(path, **q):
    n = 0
    for row in find(path, **q):
        rid = row.get(".id")
        if rid:
            st, _ = _req("DELETE", "%s/%s" % (path, rid))
            if st in (200, 204):
                n += 1
    return n


def ensure_user(user, pw):
    # 组(幂等)
    if not find("/user/group", name=user):
        st, data = _req("PUT", "/user/group", {
            "name": user, "policy": GROUP_POLICY,
            "comment": "edge-stack 受限账号"})
        if st not in (200, 201):
            die("建组失败(HTTP %s):%s" % (st, data))
        log("已建受限组 %s。" % user)
    else:
        log("组 %s 已存在,跳过。" % user)
    # 用户(幂等)
    if not find("/user", name=user):
        st, data = _req("PUT", "/user", {
            "name": user, "group": user, "password": pw,
            "comment": "edge-stack 专用,可随时删"})
        if st not in (200, 201):
            die("建用户失败(HTTP %s):%s" % (st, data))
        log("已建受限用户 %s。" % user)
    else:
        log("用户 %s 已存在,跳过。" % user)


def delete_user(user):
    """删受限用户及同名组(需 admin 凭据);不存在则跳过。"""
    u = delete_where("/user", name=user)
    g = delete_where("/user/group", name=user)
    log("已删除账号 %s:用户 %d / 组 %d。" % (user, u, g))


def nat_add(port, toaddr):
    comment = "edge-stack npm %s" % port
    delete_where("/ip/firewall/nat", comment=comment)
    # 匹配"目的地址是路由器自己"而不是 in-interface=WAN:外网打公网IP、内网打公网IP(回流)都能转;
    # 回流的源地址伪装靠 ROS 现有的 hairpin srcnat(192.168.1.0/24 → 192.168.1.0/24)。
    st, data = _req("PUT", "/ip/firewall/nat", {
        "chain": "dstnat", "action": "dst-nat", "protocol": "tcp",
        "dst-port": str(port), "dst-address-type": "local",
        "to-addresses": toaddr, "to-ports": "443", "comment": comment})
    if st not in (200, 201):
        die("加 dstnat 失败(HTTP %s):%s" % (st, data))
    log("已加 dstnat:%s → %s:443(%s)。" % (port, toaddr, comment))


def nat_del(port):
    n = delete_where("/ip/firewall/nat", comment="edge-stack npm %s" % port)
    log("已删 dstnat %d 条。" % n)


def cleanup(port, addrlist):
    a = delete_where("/ip/firewall/nat", comment="edge-stack npm %s" % port)
    b = delete_where("/ip/firewall/raw", **{"src-address-list": addrlist})
    c = delete_where("/ip/firewall/address-list", list=addrlist)
    log("清理完成:dstnat %d / raw %d / address-list %d 条。" % (a, b, c))


def main():
    if not (URL and USER and PASS):
        die("缺少 ROS_REST_URL/USER/PASS")
    if len(sys.argv) < 2:
        die("缺少动作参数")
    act = sys.argv[1]
    a = sys.argv[2:]
    if act == "ping":
        ping()
    elif act == "ensure-user":
        ensure_user(a[0], a[1])
    elif act == "delete-user":
        delete_user(a[0])
    elif act == "nat-add":
        nat_add(a[0], a[1])
    elif act == "nat-del":
        nat_del(a[0])
    elif act == "cleanup":
        cleanup(a[0], a[1])
    else:
        die("未知动作:%s" % act)


if __name__ == "__main__":
    main()
