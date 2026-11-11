package model

// GuardLog 是域名鉴权拒绝时的不可变快照。
type GuardLog struct {
	EventKey  string `json:"-"`
	Hits      int64  `json:"hits"`
	LastTS    int64  `json:"last_ts"`
	ID        int64  `gorm:"primaryKey" json:"id"`
	TS        int64  `json:"ts"`
	IP        string `json:"ip"`
	Host      string `json:"host"`
	Path      string `json:"path"`
	Method    string `json:"method"`
	UA        string `json:"ua"`
	RuleHost  string `json:"rule_host"`
	Mode      string `json:"mode"`
	Reason    string `json:"reason"`
	Geo       string `json:"geo"`
	CC        string `json:"cc"`
	Tag       string `json:"tag"`
	AllowIPs  string `json:"allow_ips"`
	AllowTags string `json:"allow_tags"`
}
