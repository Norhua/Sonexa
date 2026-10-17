package acceptance_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const internalModule = "github.com/norhua/sonexa/internal/"

func capabilityBusiness(path string) bool {
	return strings.HasPrefix(path, "lyrics/") || strings.HasPrefix(path, "covers/") ||
		path == "app/bindings_lyrics.go" || path == "app/bindings_cover_search.go"
}

// Inspect production ASTs, including files excluded by today's platform/build
// tags. Comments and adapter fixtures do not establish production dependencies.
func capabilityViolations(path string, file *ast.File) []string {
	var problems []string
	business := capabilityBusiness(path)
	parent := filepath.ToSlash(filepath.Dir(path)) == "provider"
	for _, spec := range file.Imports {
		imported, _ := strconv.Unquote(spec.Path.Value)
		if (business && strings.HasPrefix(imported, internalModule+"provider/")) ||
			(parent && strings.HasPrefix(imported, internalModule) && imported != internalModule+"domain") {
			problems = append(problems, "反向依赖: "+imported)
		}
		if business {
			switch imported {
			case "encoding/json", "encoding/base64", "crypto/aes", "crypto/des", "crypto/rsa", "compress/zlib":
				problems = append(problems, "上游响应解析/加密应在适配器: "+imported)
			}
		}
	}
	if !business {
		return problems
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if literal, ok := n.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			value, _ := strconv.Unquote(literal.Value)
			lower := strings.ToLower(value)
			for _, protocol := range []string{"https://", "http://", "musicu.fcg", "fcg_query_lyric", "dosearchforqqmusicdesktop", "getplaylyricinfo", "weapi/", "encseckey", "accesskey", "json:\"songmid", "json:\"qrc"} {
				if strings.Contains(lower, protocol) {
					problems = append(problems, "上游协议字符串: "+protocol)
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
		if !capabilityBusiness(rel) && filepath.ToSlash(filepath.Dir(rel)) != "provider" {
			return nil
		}
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
	for _, area := range []string{"lyrics", "covers", "provider", "app"} {
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

func TestCapabilityBoundaryCheckerRejectsRegressions(t *testing.T) {
	for _, tc := range []struct {
		path, source string
		bad          bool
	}{
		{"lyrics/service.go", `import "github.com/norhua/sonexa/internal/provider/qqmusic"`, true},
		{"covers/sources/search.go", `import "encoding/json"`, true},
		{"provider/new.go", `import "github.com/norhua/sonexa/internal/lyrics"`, true},
		{"provider/new.go", `import "github.com/norhua/sonexa/internal/provider/caa"`, true},
		{"provider/new.go", `import "github.com/norhua/sonexa/internal/domain"`, false},
		{"app/bindings_lyrics.go", `var url = "https://y.qq.com/"`, true},
		{"covers/search.go", `func f(source string) { if source == "qq" {} }`, true},
		{"app/bindings_cover_search.go", `func f(source string) { switch source { case "163": } }`, true},
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
