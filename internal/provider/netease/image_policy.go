package netease

import "github.com/norhua/sonexa/internal/provider"

func (*Provider) ImagePolicy() (*provider.ImagePolicy, error) {
	return provider.NewImagePolicy([]provider.ImageHost{{Host: "music.126.net", Subdomains: true}}, 9)
}
