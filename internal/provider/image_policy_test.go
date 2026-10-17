package provider

import "testing"

func TestImagePolicyHostBoundariesAndImmutability(t *testing.T) {
	hosts := []ImageHost{{Host: "exact.test"}, {Host: "cdn.test", Subdomains: true}}
	p, err := NewImagePolicy(hosts, 2)
	if err != nil {
		t.Fatal(err)
	}
	hosts[0].Host = "evil.test"
	for _, raw := range []string{"https://exact.test/a", "https://cdn.test/a", "https://img.cdn.test/a", "https://EXACT.TEST:443/a"} {
		if err := p.CheckURL(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{"https://sub.exact.test/a", "https://cdn.test.evil.test/a", "https://evilcdn.test/a", "http://cdn.test/a", "https://user@cdn.test/a", "https://cdn.test:444/a", "https://127.0.0.1/a", "file:///a", "https://evil.test/a"} {
		if p.CheckURL(raw) == nil {
			t.Fatal("unsafe URL allowed", raw)
		}
	}
	if p.CheckRedirect("https://cdn.test/a", 2) != nil || p.CheckRedirect("https://cdn.test/a", 3) == nil {
		t.Fatal("redirect bound changed")
	}
	var missing *ImagePolicy
	if missing.CheckURL("https://cdn.test/a") == nil {
		t.Fatal("missing policy accepted")
	}
	for _, host := range []string{"", "*", ".test", "test.", "a..test", "test:443", "https://test", "127.0.0.1", "a/test", "-a.test"} {
		if _, err := NewImagePolicy([]ImageHost{{Host: host}}, 2); err == nil {
			t.Fatal("invalid rule", host)
		}
	}
	for _, n := range []int{-1, 11} {
		if _, err := NewImagePolicy([]ImageHost{{Host: "test"}}, n); err == nil {
			t.Fatal(n)
		}
	}
}

func TestImagePoliciesNeverUseAnotherSource(t *testing.T) {
	p, _ := NewImagePolicy([]ImageHost{{Host: "one.test"}}, 2)
	q, _ := NewImagePolicy([]ImageHost{{Host: "two.test"}}, 2)
	input := map[string]*ImagePolicy{"one": p, "two": q}
	r, err := NewImagePolicies(input)
	if err != nil {
		t.Fatal(err)
	}
	input["one"] = q
	if r.For("one") != p || r.For("one").CheckURL("https://two.test/a") == nil || r.For("missing") != nil {
		t.Fatal("source isolation lost")
	}
	if _, err := NewImagePolicies(map[string]*ImagePolicy{"one": nil}); err == nil {
		t.Fatal("nil policy accepted")
	}
	if p.Key() == q.Key() {
		t.Fatal("different policies share jobs")
	}
}
