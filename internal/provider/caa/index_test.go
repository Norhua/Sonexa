package caa

import (
	"reflect"
	"testing"
)

func TestIndexProjectionsShareStructuralValidation(t *testing.T) {
	url := CoverArtArchiveBaseURL + "/release/rel"
	for _, body := range []string{`broken`, `null`, `{}`, `{"images":null}`, `{"images":{}}`, `{"images":[{"front":"invalid"}]}`} {
		t.Run(body, func(t *testing.T) {
			_, catalogErr := ParseCoverCatalog([]byte(body))
			_, handled, thumbnailErr := ParseCAAThumbnailIndex(url, []byte(body))
			if !handled || catalogErr == nil || thumbnailErr == nil || catalogErr.Error() != thumbnailErr.Error() {
				t.Fatalf("结构校验不一致: catalog=%v thumbnail=%v handled=%t", catalogErr, thumbnailErr, handled)
			}
		})
	}
	catalog, err := ParseCoverCatalog([]byte(`{"images":[]}`))
	if err != nil || len(catalog.Images) != 0 {
		t.Fatal(catalog, err)
	}
	urls, handled, err := ParseCAAThumbnailIndex(url, []byte(`{"images":[]}`))
	if !handled || err != nil || len(urls) != 0 {
		t.Fatal(urls, handled, err)
	}
}

func TestIndexProjectionsKeepDistinctIdentityRequirements(t *testing.T) {
	// A thumbnail fallback can use a URL without inventing a persistent image ID.
	body := []byte(`{"images":[{"front":true,"image":"http://archive.org/front.jpg"}]}`)
	if _, err := ParseCoverCatalog(body); err == nil {
		t.Fatal("目录仍应拒绝缺失身份")
	}
	urls, handled, err := ParseCAAThumbnailIndex(CoverArtArchiveBaseURL+"/release/rel", body)
	if !handled || err != nil || !reflect.DeepEqual(urls, []string{"https://archive.org/front.jpg"}) {
		t.Fatal(urls, handled, err)
	}
}
