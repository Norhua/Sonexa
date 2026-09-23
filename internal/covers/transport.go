package covers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"syscall"
	"time"
)

// imageTransport 保留受信来源域名检查，并在 DNS 解析后、连接前拒绝私网地址。
// 禁用环境代理，避免由代理代为解析后绕过本机的地址检查。
type imageTransport struct {
	once sync.Once
	http *http.Transport
}

func publicImageAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		return fmt.Errorf("封面来源解析到非公开网络地址")
	}
	return nil
}

func (t *imageTransport) CloseIdleConnections() {
	if t.http != nil {
		t.http.CloseIdleConnections()
	}
}

func (t *imageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 保留进程注入的离线 RoundTripper，测试不需要真实 DNS/网络。
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport.RoundTrip(req)
	}
	t.once.Do(func() {
		t.http = base.Clone()
		t.http.Proxy = nil
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second,
			ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
				return publicImageAddress(address)
			}}
		t.http.DialContext = dialer.DialContext
		t.http.DialTLSContext = nil
		t.http.DialTLS = nil
	})
	return t.http.RoundTrip(req)
}
