// Package covers 实现封面获取：进程级工作池、singleflight 请求合并、
// 磁盘 LRU 缓存与 Wails AssetServer handler。
package covers

import "net/http"

// AssetHandler 返回处理 /cover 请求的 handler。
// 封面字节经本地 URL 供 <img> 加载，不经 IPC 桥回传。
func AssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}
