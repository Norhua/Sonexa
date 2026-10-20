package covers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func TestResolverUsesAdapterCatalogOrderAndReusesFallbackPreview(t *testing.T) {
	svc, _ := resolverService(t)
	keys := []domain.CoverCatalogKey{
		{Source: "custom", ObjectType: "edition", SourceRef: "native"},
		{Source: "custom", ObjectType: "series", SourceRef: "parent"},
		{Source: "custom", ObjectType: "collection", SourceRef: "root"},
	}
	var loaded []domain.CoverCatalogKey
	policy, err := provider.NewImagePolicy([]provider.ImageHost{{Host: "images.example.test"}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	policies, err := provider.NewImagePolicies(map[string]*provider.ImagePolicy{"custom": policy})
	if err != nil {
		t.Fatal(err)
	}
	svc.ConfigureSources([]provider.ArtworkStrategy{{Provider: "metadata-only", Source: "custom",
		CatalogKeys: func(ref, group string) []domain.CoverCatalogKey {
			if ref != "native" || group != "parent" {
				t.Errorf("输入身份被业务层改写: %q %q", ref, group)
			}
			return keys
		},
		Validate: func(kind, ref string) error { return nil },
		Load: func(ctx context.Context, key domain.CoverCatalogKey, _ provider.ArtworkIO) (*domain.CoverCatalogInput, error) {
			loaded = append(loaded, key)
			input := &domain.CoverCatalogInput{Key: key}
			if key == keys[2] {
				input.Images = []domain.CoverImage{{SourceImageID: "front", Front: true, PrimaryType: "Front",
					Variants: []domain.CoverVariant{{Spec: "original", URL: "https://images.example.test/front.png"}}}}
			}
			return input, nil
		},
	}}, policies, nil)
	calls := 0
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(pngBytes(250, 250)))}, nil
	})
	target := Target{Preview: true, Provider: "metadata-only", Ref: "native", GroupRef: "parent"}
	for range 2 {
		got, err := svc.Resolve(context.Background(), target, Thumbnail)
		if err != nil || got.File == nil || got.File.Width != 250 {
			t.Fatalf("%+v %v", got, err)
		}
		// Once previewed, local fallback files do not require new network access.
		svc.pool.SetGate(func() error { return &OutboundError{Reason: "disabled"} })
	}
	if !reflect.DeepEqual(loaded, keys) || calls != 1 {
		t.Fatalf("目录顺序或缓存错误: %v calls=%d", loaded, calls)
	}
}

func TestResolverStopsCatalogFallbackOnErrorCancellationOrFound(t *testing.T) {
	failure := errors.New("upstream failed")
	for _, mode := range []string{"error", "cancel", "found", "empty targets"} {
		t.Run(mode, func(t *testing.T) {
			svc, _ := resolverService(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			loads := 0
			keys := []domain.CoverCatalogKey{{Source: "only", ObjectType: "primary", SourceRef: "a"}, {Source: "only", ObjectType: "alternate", SourceRef: "b"}}
			svc.RegisterArtwork(provider.ArtworkStrategy{Provider: "only", Source: "only",
				CatalogKeys: func(string, string) []domain.CoverCatalogKey {
					if mode == "empty targets" {
						return nil
					}
					return keys
				},
				Validate: func(string, string) error { return nil },
				Load: func(context.Context, domain.CoverCatalogKey, provider.ArtworkIO) (*domain.CoverCatalogInput, error) {
					loads++
					switch mode {
					case "error":
						return nil, failure
					case "cancel":
						cancel()
						return nil, context.Canceled
					default:
						// A catalog without a preferred spec must not try another identity.
						return &domain.CoverCatalogInput{Key: keys[0], Images: []domain.CoverImage{{SourceImageID: "front", Variants: []domain.CoverVariant{{Spec: "not-preferred"}}}}}, nil
					}
				},
			})
			got, err := svc.Resolve(ctx, Target{Preview: true, Provider: "only", Ref: "native"}, Thumbnail)
			if mode == "error" && !errors.Is(err, failure) || mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(got, err)
			}
			if (mode == "found" || mode == "empty targets") && (err != nil || got.Outcome != OutcomeEmpty) {
				t.Fatal(got, err)
			}
			want := 1
			if mode == "empty targets" {
				want = 0
			}
			if loads != want {
				t.Fatalf("unexpected fallback loads=%d want=%d", loads, want)
			}
		})
	}
}
