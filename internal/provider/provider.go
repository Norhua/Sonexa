// Package provider 定义上游接口、注册表与限流。
package provider

// ProviderID 是上游的稳定标识，如 "musicbrainz" / "itunes"。
type ProviderID string
