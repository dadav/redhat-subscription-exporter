package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/oauth2"
)

const (
	DefaultTokenURL = "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token"
	DefaultApiURL   = "https://api.access.redhat.com/management/v1/subscriptions"
)

var (
	exportToFile          string
	importUrl             string
	importUsername        string
	importPassword        string
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

// FetchAllSubscriptions fetches all subscriptions
func FetchAllSubscriptions(client *http.Client, baseURL string) ([]Subscription, error) {
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

		var errResp errorResponse
		if json.Unmarshal(bodyBytes, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("API error %d: %s", errResp.Error.Code, errResp.Error.Message)
		}

		var result subscriptionsResponse
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, fmt.Errorf("decode failed: %w", err)
		}

		allSubs = append(allSubs, result.Body...)

		if len(result.Body) < limit {
			break
		}
		offset += limit
	}

	return allSubs, nil
}

func metricsLoop(token, tokenUrl, apiUrl, export, jsonUrl, jsonUser, jsonPass string, interval int64, done chan error) {
	var client *http.Client

	if jsonUrl == "" {
		ctx := context.Background()

		conf := &oauth2.Config{
			ClientID: "rhsm-api",
			Endpoint: oauth2.Endpoint{
				TokenURL: tokenUrl,
			},
		}

		ts := conf.TokenSource(ctx, &oauth2.Token{RefreshToken: token})

		// Create an HTTP client that injects the Bearer token automatically
		client = oauth2.NewClient(ctx, ts)
	} else {
		client = &http.Client{}
	}

	go func() {
		for {
			var subs []Subscription
			var err error

			if importUrl == "" {
				subs, err = FetchAllSubscriptions(client, apiUrl)
				if err != nil {
					log.Fatal(err)
				}
			} else {
				req, err := http.NewRequest("GET", jsonUrl, nil)
				if err != nil {
					log.Fatal(err)
				}

				if jsonUser != "" && jsonPass != "" {
					req.SetBasicAuth(jsonUser, jsonPass)
				}
				resp, err := client.Do(req)
				if err != nil {
					log.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					log.Fatal(err)
				}
				if err := json.Unmarshal(body, &subs); err != nil {
					log.Fatal(err)
				}
			}

			if export != "" {
				data, err := json.MarshalIndent(subs, "", "  ")
				if err != nil {
					done <- err
					return
				}
				err = os.WriteFile(export, data, 0644)
				if err != nil {
					done <- err
					return
				}
				done <- nil
				return
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

			time.Sleep(time.Duration(interval) * time.Second)
		}
	}()
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int64) int64 {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func init() {
	flag.StringVar(&exportToFile, "export", "", "Export json to given file")
	flag.StringVar(&importUrl, "import-url", "", "Import data from url")
	flag.StringVar(&importUsername, "import-username", "", "Username for import-url")
	flag.StringVar(&importPassword, "import-password", "", "Password for import-url")
	flag.Parse()
}

func main() {
	token := getEnv("RH_OFFLINE_TOKEN", "")
	if token == "" {
		fmt.Println("Please set RH_OFFLINE_TOKEN.")
		os.Exit(1)
	}

	done := make(chan error)
	metricsLoop(token, getEnv("RH_TOKEN_URL", DefaultTokenURL), getEnv("RH_API_URL", DefaultApiURL), exportToFile, importUrl, importUsername, importPassword, getEnvInt("RH_FETCH_INTERVAL", 30), done)

	if exportToFile != "" {
		err := <-done
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}

	http.Handle("/metrics", promhttp.Handler())
	fmt.Println("Listening on :2112")
	http.ListenAndServe(":2112", nil)
}
