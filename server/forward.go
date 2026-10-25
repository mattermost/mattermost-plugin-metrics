// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

// In a cluster only the scraping node has a TSDB to query. The other nodes forward the
// dashboard queries to it with a plugin cluster event, and it sends the results back to
// the node that asked with another one.
const (
	clusterEventQueryRequest  = "query_request"
	clusterEventQueryResponse = "query_response"
)

var errScraperUnavailable = errors.New("the node collecting the metrics is not available")

type clusterQueryRequest struct {
	ID      string            `json:"id"`
	ReplyTo string            `json:"reply_to"` // cluster id of the node waiting for the response
	Request BatchQueryRequest `json:"request"`
}

type clusterQueryResponse struct {
	ID       string             `json:"id"`
	Response BatchQueryResponse `json:"response"`
	Error    string             `json:"error,omitempty"`
}

// advertiseScraper keeps the cluster id of this node in the KV store while it is the scraping node,
// so that the other nodes know where to forward the queries. The key expires on its own if this
// node goes away without cleaning up.
func (p *Plugin) advertiseScraper() {
	advertise := func() {
		_, appErr := p.API.KVSetWithOptions(KVStoreScraperNodeKey, []byte(p.nodeID), mmModel.PluginKVSetOptions{
			ExpireInSeconds: int64(scraperNodeTTL / time.Second),
		})
		if appErr != nil {
			p.API.LogError("Could not advertise the scraping node", "err", appErr.Error())
		}
	}

	advertise()
	ticker := time.NewTicker(scraperNodeTTL / 3)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			advertise()
		case <-p.closeChan:
			// another node may have taken over already, only remove our own entry
			if _, appErr := p.API.KVCompareAndDelete(KVStoreScraperNodeKey, []byte(p.nodeID)); appErr != nil {
				p.API.LogWarn("Could not remove the scraping node entry", "err", appErr.Error())
			}
			return
		}
	}
}

// forwardQueryBatch sends the queries to the scraping node and waits for the results.
func (p *Plugin) forwardQueryBatch(ctx context.Context, req BatchQueryRequest) (BatchQueryResponse, error) {
	scraperNode, appErr := p.API.KVGet(KVStoreScraperNodeKey)
	if appErr != nil {
		return BatchQueryResponse{}, fmt.Errorf("could not read the scraping node: %w", appErr)
	}
	if len(scraperNode) == 0 {
		return BatchQueryResponse{}, fmt.Errorf("%w: no node is collecting metrics yet", errScraperUnavailable)
	}

	msg := clusterQueryRequest{
		ID:      mmModel.NewId(),
		ReplyTo: p.nodeID,
		Request: req,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return BatchQueryResponse{}, fmt.Errorf("could not encode the query request: %w", err)
	}

	respCh := make(chan clusterQueryResponse, 1)
	p.pendingQueriesLock.Lock()
	p.pendingQueries[msg.ID] = respCh
	p.pendingQueriesLock.Unlock()
	defer func() {
		p.pendingQueriesLock.Lock()
		delete(p.pendingQueries, msg.ID)
		p.pendingQueriesLock.Unlock()
	}()

	err = p.API.PublishPluginClusterEvent(
		mmModel.PluginClusterEvent{Id: clusterEventQueryRequest, Data: data},
		mmModel.PluginClusterEventSendOptions{
			SendType: mmModel.PluginClusterEventSendTypeReliable,
			TargetId: string(scraperNode),
		},
	)
	if err != nil {
		return BatchQueryResponse{}, fmt.Errorf("%w: %s", errScraperUnavailable, err.Error())
	}

	timer := time.NewTimer(forwardedQueryTimeout)
	defer timer.Stop()

	select {
	case resp := <-respCh:
		if resp.Error != "" {
			return BatchQueryResponse{}, fmt.Errorf("%w: %s", errScraperUnavailable, resp.Error)
		}
		return resp.Response, nil
	case <-timer.C:
		return BatchQueryResponse{}, fmt.Errorf("%w: no answer within %s", errScraperUnavailable, forwardedQueryTimeout)
	case <-ctx.Done():
		return BatchQueryResponse{}, ctx.Err()
	case <-p.closeChan:
		return BatchQueryResponse{}, errors.New("the plugin is shutting down")
	}
}

func (p *Plugin) OnPluginClusterEvent(_ *plugin.Context, ev mmModel.PluginClusterEvent) {
	switch ev.Id {
	case clusterEventQueryRequest:
		// the hook is called from the server's cluster message handler, don't block it while querying
		go p.answerForwardedQuery(ev.Data)
	case clusterEventQueryResponse:
		p.receiveForwardedQueryResponse(ev.Data)
	}
}

func (p *Plugin) answerForwardedQuery(data []byte) {
	var msg clusterQueryRequest
	if err := json.Unmarshal(data, &msg); err != nil {
		p.API.LogError("Could not decode the forwarded query request", "err", err.Error())
		return
	}

	resp := clusterQueryResponse{ID: msg.ID}
	if p.hasLocalTSDB() {
		ctx, cancel := context.WithTimeout(context.Background(), forwardedQueryTimeout)
		defer cancel()

		var err error
		resp.Response, err = p.runQueryBatch(ctx, msg.Request)
		if err != nil {
			resp.Error = err.Error()
		}
	} else {
		resp.Error = "the node is no longer collecting the metrics"
	}

	// the results can be large but compress well
	payload, err := gzipJSON(resp)
	if err != nil {
		p.API.LogError("Could not encode the forwarded query response", "err", err.Error())
		return
	}

	err = p.API.PublishPluginClusterEvent(
		mmModel.PluginClusterEvent{Id: clusterEventQueryResponse, Data: payload},
		mmModel.PluginClusterEventSendOptions{
			SendType: mmModel.PluginClusterEventSendTypeReliable,
			TargetId: msg.ReplyTo,
		},
	)
	if err != nil {
		p.API.LogError("Could not send the forwarded query response", "node", msg.ReplyTo, "err", err.Error())
	}
}

func (p *Plugin) receiveForwardedQueryResponse(data []byte) {
	var resp clusterQueryResponse
	if err := gunzipJSON(data, &resp); err != nil {
		p.API.LogError("Could not decode the forwarded query response", "err", err.Error())
		return
	}

	p.pendingQueriesLock.Lock()
	respCh, ok := p.pendingQueries[resp.ID]
	p.pendingQueriesLock.Unlock()
	if !ok {
		// the request has timed out or was canceled
		return
	}

	select {
	case respCh <- resp:
	default:
	}
}

func gzipJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzipJSON(data []byte, v any) error {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer zr.Close()
	return json.NewDecoder(io.LimitReader(zr, MaxForwardedResponseSize)).Decode(v)
}
