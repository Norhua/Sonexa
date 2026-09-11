package main

import "github.com/norhua/sonexa/internal/covers"

// covers_CacheKey 转发到封面包的缓存键构造，
// 使绑定层与前端共用同一套键。
func covers_CacheKey(sourceRef string, size int) string {
	return covers.CacheKey(sourceRef, size)
}
