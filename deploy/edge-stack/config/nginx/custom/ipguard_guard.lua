-- 域名路由快照只在后台同步；请求路径不访问网络、不缓存放行/拒绝结果。
local cjson = require("cjson.safe")
local M = {}
local cached_body, cached_rules
local last_warning = 0
local target_host, target_port = "__IPGUARD_HOST__", 28090

function M.parse(body)
    local data = cjson.decode(body)
    if type(data) ~= "table" or data.schema ~= 1 or type(data.rules) ~= "table" or #data.rules > 200 then
        return nil, "invalid rules snapshot"
    end
    local rules = { exact = {}, wildcards = {} }
    for _, rule in ipairs(data.rules) do
        if type(rule) ~= "table" or type(rule.host) ~= "string" or rule.host == "" or type(rule.enabled) ~= "boolean" then
            return nil, "invalid domain rule"
        end
        if rule.host:sub(1, 2) == "*." then
            rules.wildcards[#rules.wildcards + 1] = { suffix = rule.host:sub(2), enabled = rule.enabled }
        else
            rules.exact[rule.host] = rule.enabled
        end
    end
    return rules
end

function M.matches(rules, host)
    host = host:lower():gsub(":.*$", ""):gsub("%.$", "")
    -- 停用的精确规则也必须覆盖泛域名规则，与 Go 判定顺序一致。
    if rules.exact[host] ~= nil then return rules.exact[host] end
    for _, rule in ipairs(rules.wildcards) do
        if #host > #rule.suffix and host:sub(-#rule.suffix) == rule.suffix then
            return rule.enabled
        end
    end
    return false
end

function M.requires_check(host)
    local body = ngx.shared.ipguard_routes:get("snapshot")
    -- 初次同步前仍走原来的实时校验，避免未知状态被误当作“不受限”。
    if not body then return true end
    if body ~= cached_body then
        local rules = M.parse(body)
        if not rules then return true end
        cached_body, cached_rules = body, rules
    end
    return M.matches(cached_rules, host)
end

local function fetch()
    -- 使用内置 cosocket，避免依赖 NPM 镜像不一定包含的 resty.http。
    local sock = ngx.socket.tcp()
    sock:settimeout(1000)
    local ok, err = sock:connect(target_host, target_port)
    if not ok then sock:close(); return nil, err end
    ok, err = sock:send("GET /api/guard/rules HTTP/1.0\r\nHost: ipguard\r\nConnection: close\r\n\r\n")
    if not ok then sock:close(); return nil, err end
    local parts, size = {}, 0
    local deadline = ngx.now() + 2
    while true do
        if ngx.now() >= deadline then sock:close(); return nil, "snapshot deadline exceeded" end
        sock:settimeout(math.max(1, math.min(1000, (deadline - ngx.now()) * 1000)))
        local chunk, read_err = sock:receiveany(4096)
        if chunk then
            size = size + #chunk
            if size > 262144 then sock:close(); return nil, "snapshot too large" end
            parts[#parts + 1] = chunk
        elseif read_err == "closed" then
            break
        else
            sock:close(); return nil, read_err
        end
    end
    sock:close()
    local response = table.concat(parts)
    if not response:match("^HTTP/1%.[01] 200 ") then return nil, "snapshot HTTP status is not 200" end
    local _, header_end = response:find("\r\n\r\n", 1, true)
    if not header_end then return nil, "invalid HTTP response" end
    local body = response:sub(header_end + 1)
    local rules, parse_err = M.parse(body)
    if not rules then return nil, parse_err end
    return body
end

local function warn(err)
    if ngx.now() - last_warning >= 30 then
        last_warning = ngx.now()
        ngx.log(ngx.WARN, "ipguard domain snapshot sync failed; retaining previous snapshot: ", err)
    end
end

function M.sync(premature)
    if premature or ngx.worker.exiting() then return end
    local dict = ngx.shared.ipguard_routes
    -- reload 时新旧 worker 短暂共存，锁避免重复同步。
    if not dict:add("sync_lock", true, 5) then return end
    local ok, body, err = pcall(fetch)
    if ok and body then
        local saved, save_err = dict:safe_set("snapshot", body)
        if not saved then warn(save_err) end
    else
        warn(err or body)
    end
    dict:delete("sync_lock")
end

function M.start(host, port)
    -- 可选目标仅供隔离测试；线上使用固定内网地址。
    if host then target_host, target_port = host, port end
    if ngx.worker.id() ~= 0 then return end
    local ok, err = ngx.timer.at(0, M.sync)
    if not ok then warn(err) end
    ok, err = ngx.timer.every(2, M.sync)
    if not ok then warn(err) end
end

return M
