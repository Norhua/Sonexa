package provider

import "time"

// QuotaID 标识平台 API 预算，不是业务能力或图片来源的注册键。
// 使用同一底层身份类型便于元数据迁移；各业务不得自行添加用途前缀。
type QuotaID = ProviderID

const (
	MusicBrainzQuota QuotaID = MusicBrainz
	ITunesQuota      QuotaID = ITunes
	QQQuota          QuotaID = QQMusic
	NetEaseQuota     QuotaID = "163"
	KugouQuota       QuotaID = "kugou"
	KuwoQuota        QuotaID = "kuwo"
)

// RateSource 仅提供限流初始化所需信息，不要求完整元数据能力。
type RateSource interface {
	ID() ProviderID
	RateLimit() (int, time.Duration)
}

// ConfigureFixedQuotas 由应用装配集中调用，不由能力构造函数调用。
// MB/iTunes/QQ 的可配置速率由同一装配入口按有效设置传入。
// CAA 索引和所有图片走封面工作池，不建立 API 桶。
func (l *Limiters) ConfigureFixedQuotas() {
	for _, id := range []QuotaID{NetEaseQuota, KugouQuota, KuwoQuota} {
		l.Configure(id, 1, time.Second)
	}
}
