package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
)

type audit struct{ count int }
func (a *audit) RoundTrip(r *http.Request) (*http.Response,error) {
	a.count++
	if a.count > 3 { return nil,fmt.Errorf("request budget exhausted") }
	fmt.Printf("request %d query=%s\n",a.count,r.URL.Query().Get("query"))
	return http.DefaultTransport.RoundTrip(r)
}
func main() {
	limiters := provider.NewLimiters()
	limiters.Configure(provider.MusicBrainz,1,time.Second)
	client := provider.NewClient(limiters,nil,"Sonexa/0.1.0 ( https://github.com/norhua/sonexa )")
	client.Retry.MaxAttempts = 1
	a := &audit{}
	client.HTTP.Transport = a
	p := musicbrainz.New(client,musicbrainz.Config{Contact:"https://github.com/norhua/sonexa"},nil)
	ctx,cancel := context.WithTimeout(context.Background(),60*time.Second)
	defer cancel()
	res,err := matching.NewEngine(config.Defaults().Matching).Run(ctx,p,matching.Input{
		Title:"悲しくて lulululu (难忍的悲伤)",Artist:"早坂爱",
		Album:"『かぐや様は告らせたい-ウルトラロマンティック-』第1巻 特典CD OST Vol.1",
	})
	if err != nil { panic(err) }
	fmt.Printf("candidates=%d automatic=%d decision=%s requests=%d\n",len(res.Scored),len(matching.AutomaticCandidates(res.Scored)),res.Decision.Outcome,a.count)
	for _,sc := range res.Scored { fmt.Printf("%s title=%s score=%.1f reviewOnly=%t\n",sc.Candidate.MatchRef,*sc.Candidate.Title,sc.Score.Total,sc.Score.ReviewOnly) }
}
