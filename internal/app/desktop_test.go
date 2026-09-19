package app

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"testing/fstest"
)

func TestDesktopOptionsWiresApplication(t *testing.T) {
	assets := fstest.MapFS{"frontend/dist/index.html": {Data: []byte("Sonexa")}}
	opts := DesktopOptions("layout-test", assets)
	if opts.Title != "Sonexa" || opts.Width != 1280 || opts.Height != 860 ||
		opts.MinWidth != 1024 || opts.MinHeight != 640 {
		t.Fatalf("桌面窗口配置发生变化: %+v", opts)
	}
	if opts.Mac == nil || opts.Mac.TitleBar == nil {
		t.Fatal("缺少 macOS 标题栏配置")
	}
	if opts.AssetServer == nil || opts.AssetServer.Assets == nil || opts.AssetServer.Handler == nil {
		t.Fatal("静态资源或封面 handler 未接线")
	}
	index, err := opts.AssetServer.Assets.Open("frontend/dist/index.html")
	if err != nil {
		t.Fatalf("静态资源未传入: %v", err)
	}
	index.Close()
	if len(opts.Bind) != 1 {
		t.Fatalf("绑定数量 = %d，期望 1", len(opts.Bind))
	}
	bound, ok := opts.Bind[0].(*App)
	if !ok || bound.version != "layout-test" {
		t.Fatalf("未绑定实际的 app.App: %T", opts.Bind[0])
	}
	if opts.OnStartup == nil || opts.OnShutdown == nil {
		t.Fatal("生命周期未接线")
	}
	if reflect.ValueOf(opts.OnStartup).Pointer() != reflect.ValueOf(bound.startup).Pointer() ||
		reflect.ValueOf(opts.OnShutdown).Pointer() != reflect.ValueOf(bound.shutdown).Pointer() {
		t.Fatal("生命周期没有使用实际 App 的回调")
	}
	// 不调用 startup：它会打开用户目录。未初始化时 handler 必须安全返回 404。
	rec := httptest.NewRecorder()
	opts.AssetServer.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cover", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("未初始化封面状态 = %d，期望 404", rec.Code)
	}
}

func TestDesktopLifecycleIsNotFrontendBinding(t *testing.T) {
	appType := reflect.TypeOf(&App{})
	for _, name := range []string{"Startup", "Shutdown", "CoverAssetHandler", "DesktopOptions"} {
		if _, ok := appType.MethodByName(name); ok {
			t.Errorf("内部生命周期/HTTP 方法 %s 不应成为 App 的导出绑定", name)
		}
	}
}
