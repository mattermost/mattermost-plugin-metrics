package main

import (
	"database/sql"

	"github.com/mattermost/squirrel"
	"github.com/pkg/errors"

	"github.com/mattermost/mattermost/server/public/model"
)

func pingClusterDiscoveryTable(db *sql.DB, driverName, clusterName string) ([]*model.ClusterDiscovery, error) {
	builder := squirrel.StatementBuilder.PlaceholderFormat(getQueryPlaceholder(driverName))

	query := builder.Select("*").From("ClusterDiscovery").
		Where(squirrel.Eq{"Type": model.CDSTypeApp}).
		Where(squirrel.Eq{"ClusterName": clusterName}).
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
	if err := rows.Scan(&list); err != nil {
		return nil, err
	}

	return list, nil
}

func getQueryPlaceholder(driverName string) squirrel.PlaceholderFormat {
	if driverName == model.DatabaseDriverPostgres {
		return squirrel.Dollar
	}
	return squirrel.Question
}

func topologyChanged(a, b []*model.ClusterDiscovery) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := sliceToMap(a)
	bMap := sliceToMap(b)

	for k := range aMap {
		_, ok := bMap[k]
		if !ok {
			return false
		}
	}

	return true
}

func sliceToMap(s []*model.ClusterDiscovery) map[string]string {
	m := make(map[string]string)
	for i := range s {
		m[s[i].Id] = s[i].Hostname
	}

	return m
}
