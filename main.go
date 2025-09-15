package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/oauth2"
)

var (
	SubscriptionInfoGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redhat_subscription_info",
		Help: "Contains info about subscriptions as labels.",
	},
		[]string{"contractNumber", "subscriptionNumber", "subscriptionName", "status", "sku"})
	SubscriptionQuantityGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redhat_subscription_quantity",
		Help: "Total number of subscriptions.",
	},
		[]string{"subscriptionNumber"})
	SubscriptionStartGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redhat_subscription_start",
		Help: "Unix timestamp of subscription start date.",
	},
		[]string{"subscriptionNumber"})
	SubscriptionEndGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redhat_subscription_end",
		Help: "Unix timestamp of subscription end date.",
	},
		[]string{"subscriptionNumber"})
)

// Subscription represents one subscription entry
type Subscription struct {
	ContractNumber     string    `json:"contractNumber"`
	EndDate            time.Time `json:"endDate"`
	Quantity           string    `json:"quantity"`
	SKU                string    `json:"sku"`
	StartDate          time.Time `json:"startDate"`
	Status             string    `json:"status"`
	SubscriptionName   string    `json:"subscriptionName"`
	SubscriptionNumber string    `json:"subscriptionNumber"`
	Pools              []struct {
		Consumed int    `json:"consumed"`
		ID       string `json:"id"`
		Quantity int    `json:"quantity"`
		Type     string `json:"type"`
	} `json:"pools"`
}

// Response structure for the API
type subscriptionsResponse struct {
	Body       []Subscription `json:"body"`
	Pagination struct {
		Count  int `json:"count"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	} `json:"pagination"`
}

// errorResponse is the error payload
type errorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// FetchAllSubscriptions fetches all pages of subscriptions
func FetchAllSubscriptions(client *http.Client) ([]Subscription, error) {
	baseURL := "https://api.access.redhat.com/management/v1/subscriptions"
	limit := 50
	offset := 0
	var allSubs []Subscription

	for {
		url := fmt.Sprintf("%s?limit=%d&offset=%d", baseURL, limit, offset)
		resp, err := client.Get(url)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)

		// First check for an error payload
		var errResp errorResponse
		if json.Unmarshal(bodyBytes, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("API error %d: %s", errResp.Error.Code, errResp.Error.Message)
		}

		// Parse success response
		var result subscriptionsResponse
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("decode failed: %w", err)
		}

		allSubs = append(allSubs, result.Body...)

		// Stop if last page
		if len(result.Body) < limit {
			break
		}
		offset += limit
	}

	return allSubs, nil
}

func metricsLoop() {
	offlineToken := os.Getenv("OFFLINE_TOKEN")
	if offlineToken == "" {
		fmt.Println("OFFLINE_TOKEN env variable not set")
		return
	}

	ctx := context.Background()

	conf := &oauth2.Config{
		ClientID: "rhsm-api",
		Endpoint: oauth2.Endpoint{
			TokenURL: "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token",
		},
	}

	ts := conf.TokenSource(ctx, &oauth2.Token{RefreshToken: offlineToken})

	// Create an HTTP client that injects the Bearer token automatically
	client := oauth2.NewClient(ctx, ts)

	go func() {
		for {
			subs, err := FetchAllSubscriptions(client)
			if err != nil {
				panic(err)
			}

			for _, s := range subs {
				quantity, err := strconv.ParseFloat(s.Quantity, 64)
				if err != nil {
					continue
				}
				SubscriptionInfoGauge.With(prometheus.Labels{"contractNumber": s.ContractNumber, "subscriptionNumber": s.SubscriptionNumber, "subscriptionName": s.SubscriptionName, "status": s.Status, "sku": s.SKU}).Set(1)
				SubscriptionQuantityGauge.With(prometheus.Labels{"subscriptionNumber": s.SubscriptionNumber}).Set(quantity)
				SubscriptionStartGauge.With(prometheus.Labels{"subscriptionNumber": s.SubscriptionNumber}).Set(float64(s.StartDate.Unix()))
				SubscriptionEndGauge.With(prometheus.Labels{"subscriptionNumber": s.SubscriptionNumber}).Set(float64(s.EndDate.Unix()))
			}

			time.Sleep(30 * time.Second)
		}
	}()
}

func main() {
	metricsLoop()
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":2112", nil)
}
