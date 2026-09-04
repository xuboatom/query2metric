package scheduler

import (
	"sort"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"github.com/yolossn/query2metric/pkg/config"
	"github.com/yolossn/query2metric/pkg/query"
)

// 全局缓存：已注册的 metric 名 → GaugeVec
var registeredVecs = make(map[string]*prometheus.GaugeVec)

// metricSetter 统一 Gauge 和 GaugeVec 的 Set 行为
type metricSetter interface {
	Set(float64)
}

type labeledSetter struct {
	gaugeVec  *prometheus.GaugeVec
	labelVals []string
}

func (s *labeledSetter) Set(v float64) {
	s.gaugeVec.WithLabelValues(s.labelVals...).Set(v)
}

// sortedKeys 返回 map 的 key 按字母排序，保证 GaugeVec label 顺序一致
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// valuesForKeys 按给定 key 顺序提取 map 的值
func valuesForKeys(m map[string]string, keys []string) []string {
	vals := make([]string, len(keys))
	for i, k := range keys {
		vals[i] = m[k]
	}
	return vals
}

func makeSetter(metric config.Metric, namespace string) (metricSetter, error) {
	fullName := namespace + "_" + metric.Name

	labelNames := sortedKeys(metric.Labels)
	labelVals := valuesForKeys(metric.Labels, labelNames)

	if gv, ok := registeredVecs[fullName]; ok {
		// 同名 metric 已注册，复用 GaugeVec
		return &labeledSetter{gaugeVec: gv, labelVals: labelVals}, nil
	}

	// 第一次注册，创建 GaugeVec
	gv := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      metric.Name,
			Help:      metric.HelpString,
		},
		labelNames,
	)
	if err := prometheus.Register(gv); err != nil {
		return nil, errors.Wrap(err, "Error registering metric")
	}
	registeredVecs[fullName] = gv
	return &labeledSetter{gaugeVec: gv, labelVals: labelVals}, nil
}

func init() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(log.DebugLevel)
}

type Scheduler struct {
	conf config.Config
}

func (s Scheduler) Start() error {
	errorChan := make(chan bool, 1)
	successChan := make(chan bool, 1)
	for _, conn := range s.conf.Connections {
		var dbConnection query.CountQuery
		var err error
		switch conn.Type {
		case config.MONGO:
			dbConnection, err = query.NewMongoConn(conn.ConnectionString)
			if err != nil {
				return err
			}
		case config.SQL:
			dbConnection, err = query.NewSQLQuery(conn.ConnectionString)
			if err != nil {
				return err
			}
		default:
			continue
		}

		for _, metric := range conn.Metrics {

			setter, err := makeSetter(metric, conn.Name)
			if err != nil {
				return errors.Wrap(err, "Error creating metric setter")
			}
			ticker := time.NewTicker(time.Duration(metric.Time) * time.Second)
			run(ticker, setter, dbConnection, metric, successChan, errorChan)
		}
	}

	go errorCounter(errorChan)
	go successCounter(successChan)

	return nil
}

func run(tick *time.Ticker, setter metricSetter, quer query.CountQuery, metric config.Metric, successChan, errorChan chan bool) {

		go func() {
		for {
			select {
			case <-tick.C:
				out, err := quer.Query(metric)
				if err != nil {
					errorChan <- true
					log.WithFields(log.Fields{"db": metric.Database, "metric": metric.Name, "query": metric.Query}).Error(err)
				} else {
					setter.Set(out)
					successChan <- true
					log.WithFields(log.Fields{"db": metric.Database, "metric": metric.Name, "query": metric.Query}).Debug("success")
				}
			}
		}
	}()
}

func FromConfig(conf config.Config) Scheduler {
	return Scheduler{conf}
}

func errorCounter(errorChan chan bool) {

	errorCounter := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "query2metric",
			Name:      "error_count",
			Help:      "No of errors when converting query to metric",
		},
	)

	prometheus.MustRegister(errorCounter)
	for {
		switch {
		case <-errorChan:
			errorCounter.Inc()
		}
	}

}

func successCounter(successChan chan bool) {

	successCounter := prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "query2metric",
			Name:      "success_count",
			Help:      "No of successful queries coverted to metrics",
		},
	)

	prometheus.MustRegister(successCounter)

	for {
		switch {
		case <-successChan:
			successCounter.Inc()
		}
	}

}
