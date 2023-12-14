// This code is mostly taken from Prometheus project
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"

	"github.com/mattermost/mattermost/server/public/model"
)

func scrapeFn(ctx context.Context, url string, w io.Writer, conf *configuration) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("Accept", scrapeAcceptHeader)
	req.Header.Add("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("X-Prometheus-Scrape-Timeout-Seconds", strconv.FormatFloat(float64(*conf.ScrapeTimeoutSeconds), 'f', -1, 64))

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned HTTP status %s", resp.Status)
	}

	if *conf.BodySizeLimitBytes <= 0 {
		conf.BodySizeLimitBytes = model.NewInt64(math.MaxInt64)
	}
	if resp.Header.Get("Content-Encoding") != "gzip" {
		n, err2 := io.Copy(w, io.LimitReader(resp.Body, *conf.BodySizeLimitBytes))
		if err2 != nil {
			return "", err2
		}
		if n >= *conf.BodySizeLimitBytes {
			return "", errBodySizeLimit
		}
		return resp.Header.Get("Content-Type"), nil
	}

	buf := bufio.NewReader(resp.Body)
	gzipr, err := gzip.NewReader(buf)
	if err != nil {
		return "", err
	}

	n, err := io.Copy(w, io.LimitReader(gzipr, *conf.BodySizeLimitBytes))
	gzipr.Close()
	if err != nil {
		return "", err
	}
	if n >= *conf.BodySizeLimitBytes {
		return "", errBodySizeLimit
	}

	return resp.Header.Get("Content-Type"), nil
}
