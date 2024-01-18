package main

import (
	"database/sql"

	"github.com/mattermost/squirrel"
	"github.com/pkg/errors"

	"github.com/mattermost/mattermost/server/public/model"
)

func pingClusterDiscoveryTable(db *sql.DB, driverName, clusterName string) ([]*model.ClusterDiscovery, error) {
	var phf squirrel.PlaceholderFormat
	phf = squirrel.Question
	if driverName == model.DatabaseDriverPostgres {
		phf = squirrel.Dollar
	}

	builder := squirrel.StatementBuilder.PlaceholderFormat(phf)

	query := builder.Select("Id,Hostname").From("ClusterDiscovery").
		Where(squirrel.Eq{"Type": model.CDSTypeApp, "ClusterName": clusterName}).
		Where(squirrel.Gt{"LastPingAt": model.GetMillis() - model.CDSOfflineAfterMillis})

	queryString, args, err := query.ToSql()
	if err != nil {
		return nil, errors.Wrap(err, "cluster_discovery_tosql")
	}

	rows, err := db.Query(queryString, args...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find ClusterDiscovery")
	}
	defer rows.Close()

	list := []*model.ClusterDiscovery{}
	for rows.Next() {
		var cd model.ClusterDiscovery
		if err := rows.Scan(&cd.Id, &cd.Hostname); err != nil {
			return nil, err
		}
		list = append(list, &cd)
	}

	return list, nil
}

func topologyChanged(a, b []*model.ClusterDiscovery) bool {
	if len(a) != len(b) {
		return true
	}

	aMap := sliceToMap(a)
	bMap := sliceToMap(b)

	for k := range aMap {
		_, ok := bMap[k]
		if !ok {
			return true
		}
	}

	return false
}

func sliceToMap(s []*model.ClusterDiscovery) map[string]string {
	m := make(map[string]string)
	for i := range s {
		m[s[i].Id] = s[i].Hostname
	}

	return m
}
