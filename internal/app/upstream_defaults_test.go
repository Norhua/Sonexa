package app

import "testing"

func TestLocalUpstreamDefaultsAreInjected(t *testing.T) {
	a := NewApp("test")
	countries := a.ListCountries()
	if len(countries) == 0 || !a.SuggestCountry("ひらがな").Detected {
		t.Fatal("local defaults were not wired")
	}
	first := countries[0]
	countries[0].Code = "changed"
	if a.ListCountries()[0] != first {
		t.Fatal("binding leaked injected options")
	}
	a.countryHint = func(text string) CountryHint { return CountryHint{Country: "test", Message: text, Detected: true} }
	if got := a.SuggestCountry("sample"); got.Country != "test" || got.Message != "sample" {
		t.Fatal(got)
	}
}
