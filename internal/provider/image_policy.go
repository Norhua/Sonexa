package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ImageHost distinguishes an exact host from an explicitly trusted DNS subtree.
// Only backend adapters declare these rules, never frontend or upstream payloads.
type ImageHost struct {
	Host       string
	Subdomains bool
}

// ImagePolicy is an immutable, source-scoped image/index network policy.
// It does not grant API capabilities or change the shared cover pool quota.
type ImagePolicy struct {
	hosts        []ImageHost
	maxRedirects int
	key          string
}

func NewImagePolicy(hosts []ImageHost, maxRedirects int) (*ImagePolicy, error) {
	if len(hosts) == 0 || maxRedirects < 0 || maxRedirects > 10 {
		return nil, fmt.Errorf("图片策略必须声明主机和 0–10 次重定向上限")
	}
	copy := append([]ImageHost(nil), hosts...)
	for i, rule := range copy {
		host := strings.ToLower(rule.Host)
		if host == "" || strings.TrimSpace(host) != host || net.ParseIP(host) != nil || len(host) > 253 {
			return nil, fmt.Errorf("无效图片策略主机 %q", rule.Host)
		}
		for _, label := range strings.Split(host, ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return nil, fmt.Errorf("无效图片策略主机 %q", rule.Host)
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return nil, fmt.Errorf("无效图片策略主机 %q", rule.Host)
				}
			}
		}
		copy[i].Host = host
	}
	encoded, _ := json.Marshal(struct {
		Hosts     []ImageHost
		Redirects int
	}{copy, maxRedirects})
	hash := sha256.Sum256(encoded)
	return &ImagePolicy{hosts: copy, maxRedirects: maxRedirects, key: hex.EncodeToString(hash[:])}, nil
}

func (p *ImagePolicy) CheckURL(raw string) error {
	if p == nil {
		return fmt.Errorf("图片来源安全策略未登记")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || u.Opaque != "" {
		return fmt.Errorf("封面地址必须使用可信 HTTPS 来源")
	}
	host := strings.ToLower(u.Hostname())
	for _, rule := range p.hosts {
		if host == rule.Host || rule.Subdomains && strings.HasSuffix(host, "."+rule.Host) {
			return nil
		}
	}
	return fmt.Errorf("封面地址不在所属来源安全范围内")
}

func (p *ImagePolicy) CheckRedirect(raw string, count int) error {
	if err := p.CheckURL(raw); err != nil {
		return err
	}
	if count > p.maxRedirects {
		return fmt.Errorf("封面重定向次数过多")
	}
	return nil
}

// Key keeps requests with different policy snapshots out of the same in-flight job.
func (p *ImagePolicy) Key() string {
	if p == nil {
		return "unregistered"
	}
	return p.key
}

// ImagePolicySource is optional and independent of metadata/lyrics contracts.
type ImagePolicySource interface{ ImagePolicy() (*ImagePolicy, error) }

// ImagePolicies is a validated immutable assembly snapshot, keyed by actual image source.
type ImagePolicies struct{ bySource map[string]*ImagePolicy }

func NewImagePolicies(entries map[string]*ImagePolicy) (*ImagePolicies, error) {
	out := &ImagePolicies{bySource: make(map[string]*ImagePolicy, len(entries))}
	for source, policy := range entries {
		if strings.TrimSpace(source) == "" || policy == nil || policy.key == "" {
			return nil, fmt.Errorf("图片来源 %q 缺少有效安全策略", source)
		}
		out.bySource[source] = policy
	}
	return out, nil
}
func (p *ImagePolicies) For(source string) *ImagePolicy {
	if p == nil {
		return nil
	}
	return p.bySource[source]
}
