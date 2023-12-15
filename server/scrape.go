// This code is mostly taken from Prometheus project
package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/pkg/errors"
)

var errBodySizeLimit = errors.New("body size limit exceeded")

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

	var rc io.ReadCloser
	if resp.Header.Get("Content-Encoding") != "gzip" {
		rc = resp.Body
	} else {
		buf := bufio.NewReader(resp.Body)
		rc, err = gzip.NewReader(buf)
		if err != nil {
			return "", err
		}
	}
	defer rc.Close()

	n, err2 := io.Copy(w, io.LimitReader(rc, *conf.BodySizeLimitBytes))
	if err2 != nil {
		return "", err2
	}

	if n >= *conf.BodySizeLimitBytes {
		return "", errBodySizeLimit
	}

	return resp.Header.Get("Content-Type"), nil
}
