package covers

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func solidPNG(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		panic(err)
	}
	return out.Bytes()
}

func TestAlbumFilesNamesAndIsolation(t *testing.T) {
	root := t.TempDir()
	files, err := NewAlbumFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	red := solidPNG(800, 800, color.RGBA{R: 255, A: 255})
	blue := solidPNG(800, 800, color.RGBA{B: 255, A: 255})
	identity := FileIdentity{ReleaseID: 42, Source: "qq", ImageID: "album-a", Type: "Front"}
	first, err := files.Save(identity, red)
	if err != nil || first.RelativePath != "albums/release-42/Front-qq-800.png" || !first.Created {
		t.Fatalf("文件名不正确: %+v %v", first, err)
	}
	// 名称中没有可变专辑标题，相同名称的另一歌集由 ID 隔离。
	identity.ReleaseID = 43
	other, err := files.Save(identity, red)
	if err != nil || !strings.HasPrefix(other.RelativePath, "albums/release-43/") {
		t.Fatalf("歌集目录没有隔离: %+v %v", other, err)
	}
	identity.ReleaseID = 42
	identity.ImageID = "album-b"
	second, err := files.Save(identity, blue)
	if err != nil || second.RelativePath == first.RelativePath || !strings.Contains(second.RelativePath, "Front-qq-800-") {
		t.Fatalf("同规格图片覆盖: %+v %v", second, err)
	}
	again, err := files.Save(identity, blue)
	if err != nil || again.RelativePath != second.RelativePath || again.Created {
		t.Fatalf("后缀不稳定: %+v %v", again, err)
	}
	stored, err := files.Read(first.RelativePath)
	if err != nil || !bytes.Equal(stored, red) {
		t.Fatal("第一张图片被覆盖")
	}
	identity.Source, identity.Original, identity.Type = "caa", true, "Back"
	original, err := files.Save(identity, solidPNG(1200, 800, color.RGBA{A: 255}))
	if err != nil || !strings.HasSuffix(original.RelativePath, "Back-caa-original-1200x800.png") {
		t.Fatalf("非方原图命名不正确: %+v %v", original, err)
	}
	identity.ReleaseID, identity.ObjectRef = 0, "release:r"
	preview, err := files.Save(identity, red)
	if err != nil || !strings.HasPrefix(preview.RelativePath, "cache/sources/caa-") {
		t.Fatalf("预览写进歌集目录: %+v %v", preview, err)
	}
}

func TestAlbumFilesRejectEscapes(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	files, err := NewAlbumFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	data := pngBytes(10, 20)
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../secret.png", "escape/secret.png", "/secret.png", `C:\secret.png`, "."} {
		if _, err := files.Read(name); err == nil {
			t.Errorf("可越界读取: %s", name)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "albums")); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Save(FileIdentity{ReleaseID: 1, Source: "local", ImageID: "x", Type: "Front"}, data); err == nil {
		t.Fatal("可通过符号链接越界写入")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 1 {
		t.Fatal("越界写入改变了外部目录")
	}
}

func TestInspectImageFormatsAndLimits(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 300, 200)), nil); err != nil {
		t.Fatal(err)
	}
	// 由本地 Chromium canvas 生成的静态 1×1 WebP，移除非必要 ICC 块。
	webp, err := base64.StdEncoding.DecodeString("UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoBAAEAAUAmJaACdLoB+AADsAD+8ut//NgVzXPv9//S4P0uD9Lg/9KQAAA=")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		data   []byte
		format string
		w, h   int
	}{{jpg.Bytes(), "jpeg", 300, 200}, {pngBytes(800, 600), "png", 800, 600}, {webp, "webp", 1, 1}} {
		info, err := InspectImage(tc.data)
		if err != nil || info.Width != tc.w || info.Height != tc.h || info.Format != tc.format || info.Hash == "" {
			t.Fatalf("有效图片不能解码: %+v %v (format=%s)", info, err, tc.format)
		}
	}
	tooManyPixels := append([]byte(nil), pngBytes(1, 1)...)
	binary.BigEndian.PutUint32(tooManyPixels[16:20], 100_000)
	binary.BigEndian.PutUint32(tooManyPixels[20:24], 100_000)
	binary.BigEndian.PutUint32(tooManyPixels[29:33], crc32.ChecksumIEEE(tooManyPixels[12:29]))
	for _, data := range [][]byte{[]byte("<html>not jpg</html>"), pngBytes(20, 10)[:40], tooManyPixels, make([]byte, MaxImageBytes+1)} {
		if _, err := InspectImage(data); err == nil {
			t.Fatal("非法、截断或超限图片被接受")
		}
	}
	if _, err := ReadImageBytes(io.LimitReader(zeroReader{}, MaxImageBytes+1)); err == nil {
		t.Fatal("超限读取被静默截断")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestAlbumFilesBadIdentityAndUnwritableDirectory(t *testing.T) {
	root := t.TempDir()
	files, err := NewAlbumFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	for _, identity := range []FileIdentity{
		{ReleaseID: 1, Source: "qq", ImageID: "x", Type: "../../Front"},
		{ReleaseID: 1, Source: "../qq", ImageID: "x", Type: "Front"},
		{ReleaseID: 0, Source: "qq", ImageID: "x", Type: "Front"},
	} {
		if _, err := files.Save(identity, pngBytes(10, 10)); err == nil {
			t.Fatal("非法身份不应生成文件")
		}
	}
	if err := os.WriteFile(filepath.Join(root, "albums"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Save(FileIdentity{ReleaseID: 1, Source: "local", ImageID: "x", Type: "Front"}, pngBytes(10, 10)); err == nil {
		t.Fatal("目录不可写应失败")
	}
}

func TestReleaseDirUsesSanitizedTitle(t *testing.T) {
	cases := map[string]string{
		"心の羽根":                    "albums/release-7-心の羽根",
		`AC/DC: Back "in" Black?`: "albums/release-7-AC_DC_ Back _in_ Black_",
		"  ..  ":                  "albums/release-7",
		"Vol. 1.":                 "albums/release-7-Vol. 1",
	}
	for title, want := range cases {
		if got := releaseDir(7, title); got != want || !safeRelativePath(got) {
			t.Errorf("%q => %q", title, got)
		}
	}
	if got := releaseDir(7, strings.Repeat("长", 200)); len([]rune(got)) != len([]rune("albums/release-7-"))+maxDirTitleRunes {
		t.Error("目录名未截断", got)
	}
}
