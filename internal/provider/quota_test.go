package provider

import (
	"context"
	"testing"
	"time"
)

func TestFixedQuotasReuseBucketsAcrossClients(t *testing.T) {
	l := NewLimiters()
	l.ConfigureFixedQuotas()
	for _, id := range []QuotaID{NetEaseQuota, KugouQuota, KuwoQuota} {
		if l.bucketFor(id).Limit() != 1 || l.bucketFor(id).Burst() != 1 {
			t.Fatal("平台默认配额不符", id)
		}
	}
	first := NewClient(l, nil, "first")
	second := NewClient(l, nil, "second")
	if first.Limiters != second.Limiters {
		t.Fatal("实例未共享限流集合")
	}
	if err := first.Limiters.Wait(context.Background(), NetEaseQuota); err != nil {
		t.Fatal(err)
	}
	before := l.bucketFor(NetEaseQuota)
	l.ConfigureFixedQuotas()
	if before != l.bucketFor(NetEaseQuota) {
		t.Fatal("配置替换了桶")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := second.Limiters.Wait(ctx, NetEaseQuota); err == nil {
		t.Fatal("新实例获得额外令牌")
	}
	if err := second.Limiters.Wait(ctx, KugouQuota); err != nil {
		t.Fatal("其他平台被挤占", err)
	}
}

func TestReconfigurePreservesReservationAndQueueOwnership(t *testing.T) {
	l := NewLimiters()
	l.Configure(QQQuota, 1, 150*time.Millisecond)
	bucket, gate := l.bucketFor(QQQuota), l.gateFor(QQQuota)
	if err := l.Wait(context.Background(), QQQuota); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first := make(chan error, 1)
	started := time.Now()
	go func() { first <- l.Wait(ctx, QQQuota) }()
	// Wait until the first request owns the gate and has made its reservation.
	deadline := time.Now().Add(time.Second)
	for bucket.Tokens() >= 0 {
		if time.Now().After(deadline) {
			t.Fatal("未获得预约")
		}
		time.Sleep(time.Millisecond)
	}
	second := make(chan error, 1)
	go func() { second <- l.Wait(ctx, QQQuota) }()
	for {
		gate.mu.Lock()
		queued := len(gate.background)
		gate.mu.Unlock()
		if queued == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("未进入队列")
		}
		time.Sleep(time.Millisecond)
	}
	l.Configure(QQQuota, 1, 40*time.Millisecond)
	if l.bucketFor(QQQuota) != bucket || l.gateFor(QQQuota) != gate {
		t.Fatal("变速更换了桶或队列")
	}
	gate.mu.Lock()
	queued, busy := len(gate.background), gate.busy
	gate.mu.Unlock()
	if !busy || queued != 1 {
		t.Fatal("排队权被重置")
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 130*time.Millisecond {
		t.Fatal("既有预约被提前放行")
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if bucket.Limit() != 25 || bucket.Burst() != 1 {
		t.Fatal("新速率未原位生效")
	}
}
