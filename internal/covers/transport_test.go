package covers

import "testing"

func TestImageTransportRejectsResolvedPrivateAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1:443", "10.1.1.1:443", "172.16.0.1:443", "192.168.1.1:443", "169.254.169.254:443", "100.64.1.1:443", "[::1]:443", "[::ffff:127.0.0.1]:443", "[fc00::1]:443", "[fe80::1]:443", "[ff02::1]:443", "0.0.0.0:443", "224.0.0.1:443"} {
		if err := publicImageAddress(address); err == nil {
			t.Error("接受了非公开地址", address)
		}
	}
	for _, address := range []string{"8.8.8.8:443", "[2606:4700:4700::1111]:443"} {
		if err := publicImageAddress(address); err != nil {
			t.Error(address, err)
		}
	}
}
