package query

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/mongodb/mongo-tools-common/bsonutil"
	"github.com/pkg/errors"
	"github.com/yolossn/query2metric/pkg/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoQuery struct {
	connection string
	client     *mongo.Client
}

func NewMongoConn(connnectionURL string) (CountQuery, error) {
	connnectionString := os.Getenv(connnectionURL)
	if connnectionString == "" {
		return nil, ENV_NOT_SET
	}

	ctx := context.Background()

	mongoClient, err := mongo.NewClient(options.Client().ApplyURI(connnectionString))
	if err != nil {
		return nil, errors.Wrap(err, "Connection error")
	}

	err = mongoClient.Connect(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "Connection error")
	}

	return &mongoQuery{connnectionURL, mongoClient}, err
}

func (m *mongoQuery) Query(metric config.Metric) (float64, error) {
	ctx := context.Background()
	query := map[string]interface{}{}
	if metric.Query != "" {
		err := json.Unmarshal([]byte(metric.Query), &query)
		if err != nil {
			return 0, err
		}
	}
	bsonQuery, err := bsonutil.ConvertLegacyExtJSONValueToBSON(query)
	if err != nil {
		return 0, err
	}

	mode := metric.Mode
	if mode == "" {
		mode = "count"
	}

	switch mode {
	case "value":
		return m.queryField(ctx, metric.Database, metric.Collection, metric.Field, bsonQuery)
	default: // "count"
		return m.queryCount(ctx, metric.Database, metric.Collection, bsonQuery)
	}
}

func (m *mongoQuery) queryCount(ctx context.Context, db, collection string, filter interface{}) (float64, error) {
	count, err := m.client.Database(db).Collection(collection).CountDocuments(ctx, filter)
	if err != nil {
		return 0, err
	}
	return float64(count), nil
}

func (m *mongoQuery) queryField(ctx context.Context, db, collection, field string, filter interface{}) (float64, error) {
	if field == "" {
		return 0, errors.New("field must be specified when mode is 'value'")
	}

	result := m.client.Database(db).Collection(collection).FindOne(ctx, filter)
	if result.Err() != nil {
		return 0, errors.Wrap(result.Err(), "FindOne error")
	}

	var doc bson.M
	if err := result.Decode(&doc); err != nil {
		return 0, errors.Wrap(err, "Decode error")
	}

	val, err := extractField(doc, field)
	if err != nil {
		return 0, err
	}

	return toFloat64(val)
}

// extractField retrieves a value from a nested map using dot-separated path (e.g. "stats.cpu_usage")
func extractField(doc bson.M, path string) (interface{}, error) {
	parts := strings.Split(path, ".")
	var current interface{} = doc
	for i, part := range parts {
		m, ok := current.(bson.M)
		if !ok {
			return nil, errors.Errorf("cannot traverse path %q: not a document at segment %q", path, part)
		}
		v, exists := m[part]
		if !exists {
			return nil, errors.Errorf("field %q not found in document", path)
		}
		current = v
		_ = i
	}
	return current, nil
}

// toFloat64 converts a BSON value to float64 for Prometheus metrics
func toFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int:
		return float64(n), nil
	case float32:
		return float64(n), nil
	case float64:
		return n, nil
	case bool:
		if n {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, errors.Errorf("cannot convert string %q to float64", n)
		}
		return f, nil
	default:
		return 0, errors.Errorf("unsupported type %T for metric value", v)
	}
}
