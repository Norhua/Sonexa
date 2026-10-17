package caa

import "github.com/norhua/sonexa/internal/provider"

func (*Provider) ImagePolicy() (*provider.ImagePolicy, error) {
	return provider.NewImagePolicy([]provider.ImageHost{{Host: "coverartarchive.org", Subdomains: true}, {Host: "archive.org", Subdomains: true}}, 9)
}
