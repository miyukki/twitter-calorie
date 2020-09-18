package main

import (
	"context"
	"flag"
	"log"
	"math"
	"os"
	"os/signal"
	"sync/atomic"
	"time"

	"github.com/dghubble/go-twitter/twitter"
	"github.com/hypebeast/go-osc/osc"
	"golang.org/x/oauth2/clientcredentials"
)

type CalorieScaleParam struct {
	Threshold           int
	Keyword             string
	OSCHost             string
	OSCPort             int
	TwitterClientID     string
	TwitterClientSecret string
}

func newCalorieScale(ctx context.Context, param *CalorieScaleParam) *calorieScale {
	oscClient := osc.NewClient(param.OSCHost, param.OSCPort)

	config := &clientcredentials.Config{
		ClientID:     param.TwitterClientID,
		ClientSecret: param.TwitterClientSecret,
		TokenURL:     "https://api.twitter.com/oauth2/token",
	}
	httpClient := config.Client(ctx)
	twitterClient := twitter.NewClient(httpClient)

	return &calorieScale{
		ctx:           ctx,
		threshold:     param.Threshold,
		keyword:       param.Keyword,
		calorie:       atomic.Value{},
		twitterClient: twitterClient,
		oscClient:     oscClient,
		oscInterval:   time.Second,
	}
}

type calorieScale struct {
	ctx           context.Context
	threshold     int
	keyword       string
	calorie       atomic.Value
	twitterClient *twitter.Client
	oscClient     *osc.Client
	oscInterval   time.Duration
}

func (s *calorieScale) Start() {
	log.Printf("Starting...\n")

	go func() {
		ticker := time.NewTicker(s.oscInterval)
		for {
			select {
			case <-ticker.C:
				s.sendCalorie()
			case <-s.ctx.Done():
				ticker.Stop()
				break
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(6 * time.Second)
		for {
			select {
			case <-ticker.C:
				s.calculateCalorie()
			case <-s.ctx.Done():
				ticker.Stop()
				break
			}
		}
	}()
}

func (s *calorieScale) sendCalorie() {
	calorie := s.calorie.Load()
	if calorie == nil {
		return
	}

	msg := osc.NewMessage("/calorie")
	msg.Append(calorie.(int32))
	if err := s.oscClient.Send(msg); err != nil {
		log.Printf("An error occured on send osc message: %+v\n", err)
		return
	}
}

func (s *calorieScale) calculateCalorie() {
	result, _, err := s.twitterClient.Search.Tweets(&twitter.SearchTweetParams{
		Query:      s.keyword,
		ResultType: "recent",
		Count:      100,
	})
	if err != nil {
		log.Printf("An error occured on gathering tweets: %+v\n", err)
		return
	}

	var sum float64
	for i := len(result.Statuses) - 2; 0 <= i; i-- {
		next := result.Statuses[i]
		prev := result.Statuses[i+1]
		nextTime, err := next.CreatedAtTime()
		if err != nil {
			log.Printf("An error occured on next.CreatedAtTime() v=%s: %+v\n", next.CreatedAt, err)
			return
		}

		prevTime, err := prev.CreatedAtTime()
		if err != nil {
			log.Printf("An error occured on prev.CreatedAtTime() v=%s: %+v\n", prev.CreatedAt, err)
			return
		}

		diff := nextTime.Sub(prevTime).Seconds()
		if diff < 0 {
			diff = 0
		}

		sum += diff
	}

	avgInterval := sum / float64(len(result.Statuses)-1)
	t := 1 - math.Min(1, avgInterval/float64(s.threshold))
	calorie := int32(easeInOutCubic(t) * 100)

	log.Printf("Calculated keyword=%s tweets=%d avgInterval=%f, calorie=%d\n",
		s.keyword, len(result.Statuses), avgInterval, calorie)
	s.calorie.Store(calorie)
}

func easeInOutCubic(x float64) float64 {
	if x < .5 {
		return 4 * x * x * x
	} else {
		return (x-1)*(2*x-2)*(2*x-2) + 1
	}
}

func main() {
	var (
		threshold           = flag.Int("threshold", 6, "int flag")
		keyword             = flag.String("keyword", "#youtube", "string flag")
		oscHost             = flag.String("oscHost", "localhost", "string flag")
		oscPort             = flag.Int("oscPort", 8765, "int flag")
		twitterClientID     = flag.String("twitterClientID", "-", "string flag")
		twitterClientSecret = flag.String("twitterClientSecret", "-", "string flag")
	)
	flag.Parse()

	log.Printf("Initializing... threshold=%d keyword=%s oscHost=%s oscPort=%d\n", *threshold, *keyword, *oscHost, *oscPort)
	ctx, cancel := context.WithCancel(context.Background())
	s := newCalorieScale(ctx, &CalorieScaleParam{
		Threshold:           *threshold,
		Keyword:             *keyword,
		OSCHost:             *oscHost,
		OSCPort:             *oscPort,
		TwitterClientID:     *twitterClientID,
		TwitterClientSecret: *twitterClientSecret,
	})
	go s.Start()

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
	cancel()
}
