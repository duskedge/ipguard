-- 使用 OpenResty 自带的 LuaJIT 执行：luajit deploy/tests/guard_match_test.lua deploy/edge-stack/config/nginx/custom/ipguard_guard.lua
local body
ngx = { shared = { ipguard_routes = { get = function() return body end } } }
local guard = dofile(arg[1])
assert(guard.requires_check("any.example.com"), "unknown snapshot must retain live checks")
body = '{"schema":1,"rules":[]}'
assert(not guard.requires_check("any.example.com"), "empty snapshot skips checks")
body = '{"schema":1,"rules":[{"host":"*.example.com","enabled":true},{"host":"public.example.com","enabled":false},{"host":"private.other.com","enabled":true}]}'
assert(guard.requires_check("api.example.com"))
assert(guard.requires_check("API.EXAMPLE.COM:443"))
assert(guard.requires_check("deep.api.example.com"))
assert(not guard.requires_check("public.example.com"), "disabled exact rule overrides active wildcard")
assert(not guard.requires_check("example.com"), "wildcard excludes apex")
assert(not guard.requires_check("badexample.com"), "wildcard requires dot boundary")
assert(guard.requires_check("private.other.com"))
assert(not guard.requires_check("outside.net"))
body = '{"schema":1,"rules":[{"host":"*.example.com","enabled":false},{"host":"*.sub.example.com","enabled":true},{"host":"specific.example.com","enabled":true}]}'
assert(not guard.requires_check("api.sub.example.com"), "wildcards retain backend configuration order")
assert(guard.requires_check("specific.example.com"), "exact active rule overrides disabled wildcard")
body = 'not json'
assert(guard.requires_check("any.example.com"), "invalid snapshot never silently skips checks")
body = '{"schema":2,"rules":[]}'
assert(guard.requires_check("any.example.com"), "unknown schema retains live checks")
print("PASS: unknown/empty snapshot, exact/wildcard/disabled precedence, boundaries, case, malformed data")
