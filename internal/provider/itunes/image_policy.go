package itunes

import "github.com/norhua/sonexa/internal/provider"

func (*Provider) ImagePolicy() (*provider.ImagePolicy, error) {
	return provider.NewImagePolicy([]provider.ImageHost{{Host: "mzstatic.com", Subdomains: true}}, 9)
}
