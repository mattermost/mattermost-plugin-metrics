package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"
)

func TestScrapeFn(t *testing.T) {
	registry := prometheus.NewRegistry()

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "testing",
		Name:      "post",
	})
	registry.MustRegister(counter)
	counter.Inc() // give initial bump

	buf := new(bytes.Buffer)
	cfg := &configuration{}
	cfg.SetDefaults()

	t.Run("scrape openmetrics target", func(t *testing.T) {
		ts := httptest.NewServer(promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		defer ts.Close()

		contentType, err := scrapeFn(context.Background(), ts.URL, buf, cfg)
		require.NoError(t, err)
		require.Contains(t, contentType, "text/plain")
		require.Contains(t, buf.String(), "testing_post 1")

		counter.Inc()
		_, err = scrapeFn(context.Background(), ts.URL, buf, cfg)
		require.NoError(t, err)
		require.Contains(t, buf.String(), "testing_post 2")
	})

	t.Run("no metrics endpoint", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		_, err := scrapeFn(context.Background(), ts.URL, buf, cfg)
		require.EqualError(t, err, "server returned HTTP status 404 Not Found")
	})

	t.Run("size limit exceeded", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("a", 101)))
		}))
		defer ts.Close()

		c, err := cfg.Clone()
		require.NoError(t, err)
		c.BodySizeLimitBytes = model.NewInt64(100)

		_, err = scrapeFn(context.Background(), ts.URL, buf, c)
		require.EqualError(t, err, errBodySizeLimit.Error())
	})
}
