// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"github.com/jmoiron/sqlx"
	"github.com/mattermost/squirrel"
	"github.com/pkg/errors"

	"github.com/mattermost/mattermost/server/public/model"
)

func pingClusterDiscoveryTable(db *sqlx.DB, clusterName string) ([]*model.ClusterDiscovery, error) {
	var phf squirrel.PlaceholderFormat
	phf = squirrel.Question
	if db.DriverName() == model.DatabaseDriverPostgres {
		phf = squirrel.Dollar
	}

	builder := squirrel.StatementBuilder.PlaceholderFormat(phf)

	query := builder.Select("id,hostname").From("ClusterDiscovery").
		Where(squirrel.Eq{"Type": model.CDSTypeApp, "ClusterName": clusterName}).
		Where(squirrel.Gt{"LastPingAt": model.GetMillis() - model.CDSOfflineAfterMillis}).
		OrderBy("Id")

	queryString, args, err := query.ToSql()
	if err != nil {
		return nil, errors.Wrap(err, "cluster_discovery_tosql")
	}

	list := []*model.ClusterDiscovery{}
	err = db.Select(&list, queryString, args...)
	if err != nil {
		return nil, err
	}

	return list, nil
}

func topologyChanged(a, b []*model.ClusterDiscovery) bool {
	if len(a) != len(b) {
		return true
	}

	for i := range a {
		if a[i].Id != b[i].Id {
			return true
		}
	}

	return false
}
