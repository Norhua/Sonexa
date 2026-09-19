package provider

import "context"

// Priority 是一次出网请求的优先级。
//
// 只有两级，因为真正需要区分的只有「用户在等」与「后台在跑」：
// 后台的补全任务可能排了几百个请求，用户点一次搜索不该等上几分钟。
type Priority int

const (
	// PriorityBackground 是后台任务（导入匹配、补全信息匹配）。
	PriorityBackground Priority = 0
	// PriorityInteractive 是用户直接触发的请求，插到全部后台请求之前。
	PriorityInteractive Priority = 1
)

// priorityContextKey 是把优先级放进 context 的键。
//
// 走 context 而不是给每个方法加参数：优先级要从绑定层一路穿过 Provider
// 与 Client 传到令牌桶，而中间这些签名都不该为它改一遍。
type priorityContextKey struct{}

// WithPriority 返回带指定优先级的 context。
func WithPriority(ctx context.Context, p Priority) context.Context {
	return context.WithValue(ctx, priorityContextKey{}, p)
}

// PriorityOf 取出 context 中的优先级，默认为后台。
//
// 默认后台是刻意的：忘记标注优先级的代价应当是「慢一点」，
// 而不是「悄悄抢在用户前面」。
func PriorityOf(ctx context.Context) Priority {
	if ctx == nil {
		return PriorityBackground
	}
	if p, ok := ctx.Value(priorityContextKey{}).(Priority); ok {
		return p
	}
	return PriorityBackground
}
