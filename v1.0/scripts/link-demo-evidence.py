#!/usr/bin/env python3
"""Register one explicitly mapped Demo user's evidence in the formal sandbox."""
import argparse
import getpass
import hashlib
import http.cookiejar
import json
import re
import unicodedata
import sys
import uuid
from pathlib import Path
from urllib import error, request
from urllib.parse import urlsplit

SANDBOX = "https://v-2077.com/kaloq-v1"


def required_text(obj, field, maximum):
    value = obj.get(field)
    if (not isinstance(value, str) or not value or value != value.strip()
            or len(value.encode("utf-8")) > maximum
            or any(unicodedata.category(ch) == "Cc" for ch in value)):
        raise ValueError(f"缺少或无效的字段 {field}")
    return value


def bindings(args):
    if not re.fullmatch(r"[a-z0-9][a-z0-9._-]{0,63}", args.instance):
        raise ValueError("沙盒实例代码无效")
    required_text({"account_scope": args.account_scope}, "account_scope", 160)
    state = json.loads(args.file.read_text())
    users = [u for u in state["users"] if u.get("id") == args.user_id]
    if len(users) != 1 or users[0].get("removed"):
        raise ValueError("旧用户不存在、存在重名 ID 或已移除")
    user = users[0]
    unit = required_text(user, "unit_id", 160)
    shared = dict(provider_code="pyvio", instance_code=args.instance,
                  profile_version=args.profile_version)
    items = [dict(**shared, kind="enrollment", label="Demo 开户记录",
                  account_scope=args.account_scope, external_id=unit, asset="")]
    if args.include_va:
        flow = user.get("flow") or {}
        receipt = flow.get("va_data")
        if not isinstance(receipt, dict):
            raise ValueError("缺少 VA 回执，可先单独登记开户记录")
        ga_id = required_text(flow, "ga_id", 256)
        if required_text(receipt, "ga_id", 256) != ga_id or receipt.get("unit_id", unit) != unit:
            raise ValueError("VA 回执与所选用户的账号或 VA 标识不符")
        asset = required_text(receipt, "currency", 3).upper()
        if flow.get("currency") and flow["currency"].upper() != asset:
            raise ValueError("申请币种与 VA 回执不符，请核对原始记录")
        if asset not in {"USD", "HKD"}:
            raise ValueError("所选 VA 的币种超出本期登记范围")
        items.append(dict(**shared, kind="virtual_account", label="Demo VA 记录",
                          account_scope="", external_id=ga_id,
                          asset=asset))
    for item in items:
        source = (user["flow"]["va_data"] if item["kind"] == "virtual_account" else
                  {key: user.get(key) for key in ("unit_id", "client_id", "partner_user_id", "status", "updated_at")})
        projection = [args.user_id, item["kind"], item["account_scope"], item["external_id"], item["asset"], source]
        digest = hashlib.sha256(json.dumps(projection, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        item["evidence_ref"] = f"demo-state:v1:sha256:{digest}"
    return items


class NoRedirect(request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def register(args, items):
    base = args.base.rstrip("/")
    u = urlsplit(base)
    if base != SANDBOX and not (u.scheme == "http" and u.hostname in ("127.0.0.1", "localhost")
                                and not any((u.path, u.query, u.fragment, u.username, u.password))):
        raise ValueError("仅支持正式沙盒或回环测试地址")
    client = request.build_opener(NoRedirect(), request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    csrf = ""

    def api(path, body=None):
        headers = {"Origin": f"{u.scheme}://{u.netloc}", "Content-Type": "application/json",
                   "User-Agent": "Kaloq-Evidence-Link/1", "X-CSRF-Token": csrf}
        payload = json.dumps(body).encode() if body is not None else None
        req = request.Request(base + "/api/v1" + path, data=payload, headers=headers)
        try:
            with client.open(req, timeout=15) as response:
                return json.load(response)
        except error.HTTPError as exc:
            raise ValueError(f"接口 {path} 返回 HTTP {exc.code}") from None

    try:
        stage = api("/auth/login", {"login_name": input("运营账号：").strip(), "password": getpass.getpass("密码：")})
        if stage.get("stage") != "totp":
            raise ValueError("请先在工作台完成本人认证器绑定")
        api("/auth/totp", {"code": getpass.getpass("动态码：")})
        csrf = api("/me")["csrf_token"]
        path = "/merchants/" + str(args.merchant_id)
        merchant = api(path)["merchant"]
        if (merchant["merchant_code"], merchant["row_version"]) != (args.merchant_code, args.profile_version):
            raise ValueError("正式商户编号或档案版本与本次映射不符")
        parent = api(path + "/bindings", items[0])["binding"]["id"]
        if len(items) == 2:
            api(path + "/bindings", dict(items[1], parent_binding_id=parent))
        print(f"已登记或复用 {len(items)} 条待复核资料")
    finally:
        try:
            api("/auth/logout", {})
        except (ValueError, OSError):
            pass


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("file", type=Path, help="一份受控的旧 Demo 用户状态导出")
    parser.add_argument("--user-id", required=True, help="旧 users[].id")
    parser.add_argument("--merchant-id", required=True, type=uuid.UUID)
    parser.add_argument("--merchant-code", required=True, help="经核对的正式商户编号")
    parser.add_argument("--profile-version", required=True, type=int)
    parser.add_argument("--instance", required=True, help="已核定的 Pyvio 沙盒签约账号实例代码")
    parser.add_argument("--account-scope", required=True, help="上游开户记录所属原账号作用域")
    parser.add_argument("--include-va", action="store_true")
    parser.add_argument("--apply", action="store_true", help="登录正式沙盒并写入待复核关联")
    parser.add_argument("--base", default=SANDBOX)
    args = parser.parse_args()
    if args.profile_version < 1:
        parser.error("档案版本应为正整数")
    try:
        items = bindings(args)
        if args.apply:
            register(args, items)
        else:
            print(f"预检通过：选中 1 个旧用户，拟关联 {len(items)} 条记录，未写入。")
    except (KeyError, TypeError, AttributeError, UnicodeError, ValueError, OSError) as exc:
        # Source files can contain KYC records. Never echo their content.
        print(str(exc) if type(exc) is ValueError else "读取数据或访问接口失败，请检查文件格式与连接。", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
