package acceptance_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const internalModule = "github.com/norhua/sonexa/internal/"

func capabilityBusiness(path string) bool {
	return strings.HasPrefix(path, "lyrics/") || strings.HasPrefix(path, "covers/") ||
		strings.HasPrefix(path, "app/bindings_lyrics") || path == "app/bindings_cover_search.go"
}

// Inspect production ASTs, including files excluded by today's platform/build
// tags. Comments and adapter fixtures do not establish production dependencies.
func capabilityViolations(path string, file *ast.File) []string {
	var problems []string
	business := capabilityBusiness(path)
	parent := filepath.ToSlash(filepath.Dir(path)) == "provider"
	formatTool := strings.HasPrefix(path, "lyricdoc/")
	adapter := strings.HasPrefix(path, "provider/") && !parent
	imports := map[string]string{}
	for _, spec := range file.Imports {
		imported, _ := strconv.Unquote(spec.Path.Value)
		name := filepath.Base(imported)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = imported
		if (!adapter && path != "app/upstreams.go" && strings.HasPrefix(imported, internalModule+"provider/")) ||
			(parent && strings.HasPrefix(imported, internalModule) && imported != internalModule+"domain") ||
			(formatTool && (strings.Contains(imported, ".") || imported == "net" || strings.HasPrefix(imported, "net/") || imported == "os")) {
			problems = append(problems, "反向依赖: "+imported)
		}
		if business && imported == internalModule+"lyricdoc" && name == "." {
			problems = append(problems, "业务不能点导入格式工具")
		}
		if business {
			switch imported {
			case "encoding/json", "encoding/base64", "crypto/aes", "crypto/des", "crypto/rsa", "compress/zlib":
				problems = append(problems, "上游响应解析/加密应在适配器: "+imported)
			}
		}
	}
	if !business && !formatTool {
		return problems
	}
	ast.Inspect(file, func(n ast.Node) bool {
		// Catch direct calls and aliases such as finish := doc.Build. The
		// only allowed consumer-side construction is the local file adapter.
		if selector, ok := n.(*ast.SelectorExpr); ok && business {
			if pkg, ok := selector.X.(*ast.Ident); ok && imports[pkg.Name] == internalModule+"lyricdoc" {
				switch selector.Sel.Name {
				case "Build", "BuildOnline", "Input", "TextChannel", "TrackChannel", "AbsentChannel", "UnknownChannel", "FailedChannel", "PreferredChannel", "SerializeTrack", "SerializeXLRC", "ParseXLRC", "DecodeInline", "RenderTimed", "ParseText":
					if path != "app/bindings_lyrics_input.go" || selector.Sel.Name != "ParseText" && selector.Sel.Name != "ParseXLRC" {
						problems = append(problems, "业务不能构建或修复来源文档: "+selector.Sel.Name)
					}
				}
			}
		}
		if !formatTool {
			if literal, ok := n.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				value, _ := strconv.Unquote(literal.Value)
				lower := strings.ToLower(value)
				for _, protocol := range []string{"https://", "http://", "musicu.fcg", "fcg_query_lyric", "dosearchforqqmusicdesktop", "getplaylyricinfo", "weapi/", "encseckey", "accesskey", "json:\"songmid", "json:\"qrc", "release-group", "纯音乐请欣赏", "纯音乐，请欣赏", "此歌曲暂无歌词"} {
					if strings.Contains(lower, protocol) {
						problems = append(problems, "上游协议字符串: "+protocol)
					}
				}
			}
		}
		var condition ast.Node
		switch branch := n.(type) {
		case *ast.IfStmt:
			condition = branch.Cond
		case *ast.CaseClause:
			for _, expr := range branch.List {
				problems = append(problems, sourceBranchViolations(expr)...)
			}
		}
		if condition != nil {
			problems = append(problems, sourceBranchViolations(condition)...)
		}
		return true
	})
	return problems
}

func sourceBranchViolations(node ast.Node) []string {
	var problems []string
	ast.Inspect(node, func(n ast.Node) bool {
		if literal, ok := n.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			value, _ := strconv.Unquote(literal.Value)
			switch value {
			case "qq", "163", "kugou", "kuwo", "itunes", "musicbrainz", "caa":
				problems = append(problems, "来源专有分支: "+value)
			}
		}
		if selector, ok := n.(*ast.SelectorExpr); ok {
			if name, ok := selector.X.(*ast.Ident); ok && name.Name == "provider" {
				switch selector.Sel.Name {
				case "QQMusic", "MusicBrainz", "ITunes":
					problems = append(problems, "来源专有分支: provider."+selector.Sel.Name)
				}
			}
		}
		return true
	})
	return problems
}

func TestUpstreamCapabilityDependencyBoundaries(t *testing.T) {
	counts := map[string]int{}
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel("..", path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		counts[strings.Split(rel, "/")[0]]++
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, problem := range capabilityViolations(rel, file) {
			t.Errorf("%s: %s", rel, problem)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, area := range []string{"lyrics", "lyricdoc", "covers", "provider", "app"} {
		if counts[area] == 0 {
			t.Errorf("未扫描 %s 生产源码", area)
		}
	}
}

// Removed transport paths must not reappear as unused compatibility helpers.
func TestRetiredUpstreamPathsStayRemoved(t *testing.T) {
	for _, path := range []string{"lyrics/qq.go", "lyrics/qrc.go", "lyrics/qrcdes", "covers/sources/web.go", "provider/musicbrainz/covers.go", "provider/musicbrainz/cover_catalog.go"} {
		if _, err := os.Stat(filepath.Join("..", path)); !os.IsNotExist(err) {
			t.Errorf("旧协议路径仍存在或无法检查: %s (%v)", path, err)
		}
	}
	for path, names := range map[string][]string{
		"lyrics":               {"Payload", "DocumentFromPayload", "ParseText", "ParseXLRC", "SerializeXLRC", "DecodeInline", "TextChannel", "buildDocument", "normalizeChannel", "deduplicate"},
		"provider":             {"Provider", "CoverGroup", "Head"},
		"provider/itunes":      {"ResolveArtwork", "VerifyArtwork"},
		"provider/musicbrainz": {"SearchCovers", "CoverArtProviderKey"},
		"covers/sources":       {"WebSource"},
	} {
		files, err := filepath.Glob(filepath.Join("..", path, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				var declared string
				switch node := node.(type) {
				case *ast.TypeSpec:
					declared = node.Name.Name
				case *ast.FuncDecl:
					declared = node.Name.Name
				}
				for _, retired := range names {
					if declared == retired {
						t.Errorf("%s: 已移除声明 %s 不应恢复", name, retired)
					}
				}
				return true
			})
		}
	}
}

var lyricFrontendRule = regexp.MustCompile(`(?i)(?:[=!]==?\s*["'](?:qq|163|kugou|kuwo)["']|["'](?:qq|163|kugou|kuwo)["']\s*[=!]==?|case\s+["'](?:qq|163|kugou|kuwo)["']|getplaylyricinfo|weapi/|adoptKuwoBakedTranslation)`)

func TestLyricsFrontendDoesNotInterpretSources(t *testing.T) {
	for _, path := range []string{"../../frontend/src/stores/lyricsSearch.ts", "../../frontend/src/views/LyricsSearchView.vue", "../../frontend/src/lib/lyrics.ts"} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if found := lyricFrontendRule.FindString(string(body)); found != "" {
			t.Errorf("%s: 来源内容规则 %s", path, found)
		}
	}
	for _, source := range []string{`if (source === 'kuwo') split()`, `if ('qq' !== source) parse()`, `case "163":`, `GetPlayLyricInfo`} {
		if !lyricFrontendRule.MatchString(source) {
			t.Fatalf("missed regression: %s", source)
		}
	}
	if lyricFrontendRule.MatchString(`if (channel.status === 'available') show()`) {
		t.Fatal("generic state rejected")
	}
}

func TestCapabilityBoundaryCheckerRejectsRegressions(t *testing.T) {
	for _, tc := range []struct {
		path, source string
		bad          bool
	}{
		{"lyrics/service.go", `import "github.com/norhua/sonexa/internal/provider/qqmusic"`, true},
		{"covers/sources/search.go", `import "encoding/json"`, true},
		{"provider/new.go", `import "github.com/norhua/sonexa/internal/lyrics"`, true},
		{"provider/new.go", `import "github.com/norhua/sonexa/internal/provider/caa"`, true},
		{"app/app.go", `import "github.com/norhua/sonexa/internal/provider/qqmusic"`, true},
		{"app/bindings_settings.go", `import "github.com/norhua/sonexa/internal/provider/itunes"`, true},
		{"app/upstreams.go", `import "github.com/norhua/sonexa/internal/provider/qqmusic"`, false},
		{"lyricdoc/text.go", `import "github.com/norhua/sonexa/internal/provider"`, true},
		{"lyricdoc/text.go", `import "github.com/norhua/sonexa/internal/lyrics"`, true},
		{"lyricdoc/text.go", `import "net/http"`, true},
		{"lyricdoc/text.go", `func f(source string) { if source == "kuwo" {} }`, true},
		{"lyrics/service.go", `import d "github.com/norhua/sonexa/internal/lyricdoc"; var finish = d.BuildOnline`, true},
		{"lyrics/service.go", `import d "github.com/norhua/sonexa/internal/lyricdoc"; var finish = d.Build`, true},
		{"lyrics/service.go", `import . "github.com/norhua/sonexa/internal/lyricdoc"`, true},
		{"app/bindings_lyrics.go", `import "github.com/norhua/sonexa/internal/lyricdoc"; var parse = lyricdoc.ParseText`, true},
		{"app/bindings_lyrics_input.go", `import "github.com/norhua/sonexa/internal/lyricdoc"; var parse = lyricdoc.ParseText`, false},
		{"lyrics/service.go", `import "github.com/norhua/sonexa/internal/lyricdoc"; var validate = lyricdoc.Validate`, false},
		{"provider/new.go", `import "github.com/norhua/sonexa/internal/lyricdoc"`, true},
		{"provider/new.go", `import "github.com/norhua/sonexa/internal/provider/lyricfetch"`, true},
		{"provider/new.go", `import "github.com/norhua/sonexa/internal/domain"`, false},
		{"app/bindings_lyrics.go", `var url = "https://y.qq.com/"`, true},
		{"covers/search.go", `func f(source string) { if source == "qq" {} }`, true},
		{"app/bindings_cover_search.go", `func f(source string) { switch source { case "163": } }`, true},
		{"covers/resolver.go", `var kind = "release-group"`, true},
		{"lyrics/text.go", `func f(text string) { if text == "纯音乐，请欣赏" {} }`, true},
		{"lyrics/service.go", `// https://y.qq.com/ is only a comment
func f(source string) { if source == "" {} }`, false},
	} {
		t.Run(fmt.Sprint(tc.path, "/", tc.source), func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), tc.path, "package sample\n"+tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := capabilityViolations(tc.path, file); (len(got) > 0) != tc.bad {
				t.Fatalf("violations=%v wantBad=%v", got, tc.bad)
			}
		})
	}
}
