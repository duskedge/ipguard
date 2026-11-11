// Package config 负责加载配置：config.yaml + 环境变量覆盖（IPGUARD_ 前缀，点号换下划线）。
// 整个文件缺失也能跑——默认值足以启动（容器部署的常态）。
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   Server   `mapstructure:"server"`
	Auth     Auth     `mapstructure:"auth"`
	Storage  Storage  `mapstructure:"storage"`
	Logs     Logs     `mapstructure:"logs"`
	GeoIP    GeoIP    `mapstructure:"geoip"`
	CrowdSec CrowdSec `mapstructure:"crowdsec"`
	Detect   Detect   `mapstructure:"detect"`
	Log      Log      `mapstructure:"log"`
}

type Server struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // debug | release
	// Site 只用于侧栏副标题显示（如 example.net）
	Site string `mapstructure:"site"`
	// Timezone 决定“今天”从几点开始、趋势图按哪个时区分桶
	Timezone string `mapstructure:"timezone"`
}

type Auth struct {
	Username string `mapstructure:"username"`
	// Password 与 PasswordHash 二选一；PasswordHash 为 bcrypt。都为空时拒绝启动。
	Password     string        `mapstructure:"password"`
	PasswordHash string        `mapstructure:"passwordHash"`
	JWTSecret    string        `mapstructure:"jwtSecret"` // 留空则生成并落盘到 dataDir/.jwt_key
	TokenTTL     time.Duration `mapstructure:"tokenTTL"`
}

type Storage struct {
	DataDir string `mapstructure:"dataDir"`
}

type Logs struct {
	// PreviewScheme/Port 仅用于日志图片的对外访问链接，不修改 Nginx 监听或后端端口。
	PreviewScheme string `mapstructure:"previewScheme"`
	PreviewPort   int    `mapstructure:"previewPort"`
	Dir           string `mapstructure:"dir"`
	// Patterns 是参与解析的文件名通配（只匹配文件名）。轮转出来的 .1 / .2.gz 会自动带上。
	Patterns []string `mapstructure:"patterns"`
	// Exclude 优先于 Patterns
	Exclude      []string      `mapstructure:"exclude"`
	PollInterval time.Duration `mapstructure:"pollInterval"`
	Retention    time.Duration `mapstructure:"retention"`
	MaxEntries   int           `mapstructure:"maxEntries"`
	// NPMConfDir 是 npm 的 data/nginx/proxy_host 目录（只读挂载），用来补全后端端口；不存在时忽略
	NPMConfDir string `mapstructure:"npmConfDir"`
	// BackendAliases 把后端地址换成名字，格式 "192.168.1.250=HOST,10.0.0.2=pve"
	BackendAliases string `mapstructure:"backendAliases"`
}

type GeoIP struct {
	// CityDB 为 GeoIP2 兼容的城市库（dbip-city-lite / GeoLite2-City），优先使用；
	// 不存在时退回 FallbackDB（镜像里打包的那份）。都缺失时归属地降级为“未知”。
	CityDB     string `mapstructure:"cityDB"`
	FallbackDB string `mapstructure:"fallbackDB"`
	// RegionDB 是 ip2region（xdb），用于修正国内 IP 的省市与运营商；同样先挂载、后内置
	RegionDB         string `mapstructure:"regionDB"`
	RegionFallbackDB string `mapstructure:"regionFallbackDB"`
}

type CrowdSec struct {
	URL       string        `mapstructure:"url"`
	MachineID string        `mapstructure:"machineID"`
	Password  string        `mapstructure:"password"`
	Timeout   time.Duration `mapstructure:"timeout"`
	CacheTTL  time.Duration `mapstructure:"cacheTTL"`
	// Origin 是 ipguard 写入决策时使用的 origin；cscli 会被归为“手动”
	Origin string `mapstructure:"origin"`
}

type Detect struct {
	SensitivePaths []string `mapstructure:"sensitivePaths"`
	// 同一 IP 在窗口内命中敏感路径不少于 ScanHits 次，且 4xx/5xx 占比 ≥ ScanErrRatio → 扫描
	ScanHits     int     `mapstructure:"scanHits"`
	ScanErrRatio float64 `mapstructure:"scanErrRatio"`
	// 请求数 ≥ WarnMinReq 且 4xx/5xx 占比 ≥ WarnErrRatio → 可疑
	WarnMinReq   int     `mapstructure:"warnMinReq"`
	WarnErrRatio float64 `mapstructure:"warnErrRatio"`
}

type Log struct {
	Level string `mapstructure:"level"`
}

// DefaultSensitivePaths 是常见扫描特征（小写子串匹配）。
var DefaultSensitivePaths = []string{
	"/.env", "/.git", "/.svn", "/.aws", "/.ds_store", "/.htaccess", "/.ssh",
	"wp-login", "wp-admin", "wp-config", "xmlrpc.php", "phpmyadmin", "/pma",
	"/actuator", "eval-stdin", "/vendor/phpunit", "/cgi-bin", "/boaform",
	"/server-status", "/solr", "/manager/html", "/hnap1",
	"/setup.cgi", "../", "%2e%2e", ".bak", ".sql", "/config.json",
	"/telescope", "/_profiler", "/owa/", "/remote/login", "/druid",
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.mode", "release")
	v.SetDefault("server.site", "")
	v.SetDefault("server.timezone", "Asia/Shanghai")

	// 没有默认值的 key 也要登记一个空默认值，否则 AutomaticEnv 在 Unmarshal 时读不到环境变量
	v.SetDefault("auth.username", "admin")
	v.SetDefault("auth.password", "")
	v.SetDefault("auth.passwordHash", "")
	v.SetDefault("auth.jwtSecret", "")
	v.SetDefault("auth.tokenTTL", 12*time.Hour)
	v.SetDefault("crowdsec.machineID", "")
	v.SetDefault("crowdsec.password", "")

	v.SetDefault("storage.dataDir", "./data")

	v.SetDefault("logs.previewScheme", "https")
	v.SetDefault("logs.previewPort", 443)
	v.SetDefault("logs.dir", "/logs")
	v.SetDefault("logs.patterns", []string{"*_access.log"})
	v.SetDefault("logs.exclude", []string{"fallback_stream_access.log"})
	v.SetDefault("logs.pollInterval", 5*time.Second)
	v.SetDefault("logs.retention", 7*24*time.Hour)
	v.SetDefault("logs.maxEntries", 2_000_000)
	v.SetDefault("logs.npmConfDir", "/npm-nginx/proxy_host")
	v.SetDefault("logs.backendAliases", "")

	v.SetDefault("geoip.cityDB", "./data/geoip/dbip-city-lite.mmdb")
	v.SetDefault("geoip.fallbackDB", "/app/geoip/dbip-city-lite.mmdb")
	v.SetDefault("geoip.regionDB", "./data/geoip/ip2region.xdb")
	v.SetDefault("geoip.regionFallbackDB", "/app/geoip/ip2region.xdb")

	v.SetDefault("crowdsec.url", "http://crowdsec:8080")
	v.SetDefault("crowdsec.timeout", 15*time.Second)
	v.SetDefault("crowdsec.cacheTTL", 30*time.Second)
	v.SetDefault("crowdsec.origin", "cscli")

	v.SetDefault("detect.sensitivePaths", DefaultSensitivePaths)
	v.SetDefault("detect.scanHits", 3)
	v.SetDefault("detect.scanErrRatio", 0.6)
	v.SetDefault("detect.warnMinReq", 10)
	v.SetDefault("detect.warnErrRatio", 0.3)

	v.SetDefault("log.level", "info")
}

// Load 读取配置。path 为空时依次尝试 ./config.yaml、./data/config.yaml。
func Load(path string) (*Config, error) {
	v := viper.New()
	setDefaults(v)
	v.SetEnvPrefix("IPGUARD")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./data")
	}
	if err := v.ReadInConfig(); err != nil {
		var nf viper.ConfigFileNotFoundError
		if path != "" || !asNotFound(err, &nf) {
			return nil, fmt.Errorf("读取配置: %w", err)
		}
	}

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("解析配置: %w", err)
	}
	// AutomaticEnv 对 Unmarshal 里的切片不生效（没有默认值以外的 key 时），手动补一下
	if s := v.GetString("logs.patterns"); s != "" && strings.Contains(s, ",") {
		c.Logs.Patterns = splitList(s)
	}
	if s := v.GetString("logs.exclude"); s != "" && strings.Contains(s, ",") {
		c.Logs.Exclude = splitList(s)
	}
	return &c, c.validate()
}

func asNotFound(err error, target *viper.ConfigFileNotFoundError) bool {
	if e, ok := err.(viper.ConfigFileNotFoundError); ok {
		*target = e
		return true
	}
	return false
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) validate() error {
	if c.Auth.Password == "" && c.Auth.PasswordHash == "" {
		return fmt.Errorf("未配置登录口令：设置 auth.password 或 auth.passwordHash（环境变量 IPGUARD_AUTH_PASSWORD / IPGUARD_AUTH_PASSWORDHASH）")
	}
	if c.Auth.Username == "" {
		return fmt.Errorf("auth.username 不能为空")
	}
	if _, err := time.LoadLocation(c.Server.Timezone); err != nil {
		return fmt.Errorf("server.timezone 无效: %w", err)
	}
	if c.Logs.PreviewScheme != "https" && c.Logs.PreviewScheme != "http" {
		return fmt.Errorf("logs.previewScheme 仅支持 http 或 https")
	}
	if c.Logs.PreviewPort < 1 || c.Logs.PreviewPort > 65535 {
		return fmt.Errorf("logs.previewPort 必须为 1–65535")
	}
	if c.Logs.Retention < time.Hour {
		return fmt.Errorf("logs.retention 至少 1h")
	}
	return nil
}

// Location 返回配置的时区。validate 已保证可用。
func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Server.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

// CrowdSecConfigured 报告是否填了 machine 凭据。
func (c *Config) CrowdSecConfigured() bool {
	return c.CrowdSec.URL != "" && c.CrowdSec.MachineID != "" && c.CrowdSec.Password != ""
}
