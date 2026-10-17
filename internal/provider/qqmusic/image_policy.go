package qqmusic

import "github.com/norhua/sonexa/internal/provider"

func (*Provider) ImagePolicy() (*provider.ImagePolicy, error) {
	return provider.NewImagePolicy([]provider.ImageHost{{Host: "y.qq.com", Subdomains: true}}, 9)
}
