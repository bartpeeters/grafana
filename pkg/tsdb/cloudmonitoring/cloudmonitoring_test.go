package cloudmonitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudMonitoring(t *testing.T) {
	service := &Service{}

	t.Run("Parse migrated queries from frontend and build Google Cloud Monitoring API queries", func(t *testing.T) {
		t.Run("and query has no aggregation set", func(t *testing.T) {
			qes, err := service.buildQueryExecutors(slog, baseReq())
			require.NoError(t, err)
			queries := getCloudMonitoringQueriesFromInterface(t, qes)

			require.Len(t, queries, 1)
			assert.Equal(t, "A", queries[0].RefID)
			assert.Equal(t, "aggregation.alignmentPeriod=%2B60s&aggregation.crossSeriesReducer=REDUCE_NONE&aggregation.perSeriesAligner=ALIGN_MEAN&filter=metric.type%3D%22a%2Fmetric%2Ftype%22&interval.endTime=2018-03-15T13%3A34%3A00Z&interval.startTime=2018-03-15T13%3A00%3A00Z&view=FULL", queries[0].Target)
			assert.Equal(t, 7, len(queries[0].Params))
			assert.Equal(t, "2018-03-15T13:00:00Z", queries[0].Params["interval.startTime"][0])
			assert.Equal(t, "2018-03-15T13:34:00Z", queries[0].Params["interval.endTime"][0])
			assert.Equal(t, "ALIGN_MEAN", queries[0].Params["aggregation.perSeriesAligner"][0])
			assert.Equal(t, "metric.type=\"a/metric/type\"", queries[0].Params["filter"][0])
			assert.Equal(t, "FULL", queries[0].Params["view"][0])
			assert.Equal(t, "testalias", queries[0].AliasBy)

			t.Run("and generated deep link has correct parameters", func(t *testing.T) {
				// assign resource type to query parameters to be included in the deep link filter
				// in the actual workflow this information comes from the response of the Monitoring API
				queries[0].Params.Set("resourceType", "a/resource/type")
				dl := queries[0].buildDeepLink()

				expectedTimeSelection := map[string]string{
					"timeRange": "custom",
					"start":     "2018-03-15T13:00:00Z",
					"end":       "2018-03-15T13:34:00Z",
				}
				expectedTimeSeriesFilter := map[string]interface{}{
					"perSeriesAligner": "ALIGN_MEAN",
					"filter":           "resource.type=\"a/resource/type\" metric.type=\"a/metric/type\"",
				}
				verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
			})
		})

		t.Run("and query has filters", func(t *testing.T) {
			query := baseReq()
			query.Queries[0].JSON = json.RawMessage(`{
				"metricType": "a/metric/type",
				"filters":    ["key", "=", "value", "AND", "key2", "=", "value2", "AND", "resource.type", "=", "another/resource/type"]
			}`)

			qes, err := service.buildQueryExecutors(slog, query)
			require.NoError(t, err)
			queries := getCloudMonitoringQueriesFromInterface(t, qes)
			assert.Equal(t, 1, len(queries))
			assert.Equal(t, `metric.type="a/metric/type" key="value" key2="value2" resource.type="another/resource/type"`, queries[0].Params["filter"][0])

			// assign a resource type to query parameters
			// in the actual workflow this information comes from the response of the Monitoring API
			// the deep link should not contain this resource type since another resource type is included in the query filters
			queries[0].Params.Set("resourceType", "a/resource/type")
			dl := queries[0].buildDeepLink()

			expectedTimeSelection := map[string]string{
				"timeRange": "custom",
				"start":     "2018-03-15T13:00:00Z",
				"end":       "2018-03-15T13:34:00Z",
			}
			expectedTimeSeriesFilter := map[string]interface{}{
				"filter": `metric.type="a/metric/type" key="value" key2="value2" resource.type="another/resource/type"`,
			}
			verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
		})

		t.Run("and alignmentPeriod is set to grafana-auto", func(t *testing.T) {
			t.Run("and IntervalMS is larger than 60000", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].Interval = 1000000 * time.Millisecond
				req.Queries[0].JSON = json.RawMessage(`{
					"alignmentPeriod": "grafana-auto",
					"filters":    ["key", "=", "value", "AND", "key2", "=", "value2"]
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+1000s`, queries[0].Params["aggregation.alignmentPeriod"][0])

				// assign resource type to query parameters to be included in the deep link filter
				// in the actual workflow this information comes from the response of the Monitoring API
				queries[0].Params.Set("resourceType", "a/resource/type")
				dl := queries[0].buildDeepLink()

				expectedTimeSelection := map[string]string{
					"timeRange": "custom",
					"start":     "2018-03-15T13:00:00Z",
					"end":       "2018-03-15T13:34:00Z",
				}
				expectedTimeSeriesFilter := map[string]interface{}{
					"minAlignmentPeriod": `1000s`,
				}
				verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
			})
			t.Run("and IntervalMS is less than 60000", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].Interval = 30000 * time.Millisecond
				req.Queries[0].JSON = json.RawMessage(`{
					"alignmentPeriod": "grafana-auto",
					"filters":    ["key", "=", "value", "AND", "key2", "=", "value2"]
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+60s`, queries[0].Params["aggregation.alignmentPeriod"][0])

				// assign resource type to query parameters to be included in the deep link filter
				// in the actual workflow this information comes from the response of the Monitoring API
				queries[0].Params.Set("resourceType", "a/resource/type")
				dl := queries[0].buildDeepLink()

				expectedTimeSelection := map[string]string{
					"timeRange": "custom",
					"start":     "2018-03-15T13:00:00Z",
					"end":       "2018-03-15T13:34:00Z",
				}
				expectedTimeSeriesFilter := map[string]interface{}{
					"minAlignmentPeriod": `60s`,
				}
				verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
			})
		})

		t.Run("and alignmentPeriod is set to cloud-monitoring-auto", func(t *testing.T) { // legacy
			now := time.Now().UTC()

			t.Run("and range is two hours", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].TimeRange.From = now.Add(-(time.Hour * 2))
				req.Queries[0].TimeRange.To = now
				req.Queries[0].JSON = json.RawMessage(`{
					"target": "target",
					"alignmentPeriod": "cloud-monitoring-auto"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+60s`, queries[0].Params["aggregation.alignmentPeriod"][0])
			})

			t.Run("and range is 22 hours", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].TimeRange.From = now.Add(-(time.Hour * 22))
				req.Queries[0].TimeRange.To = now
				req.Queries[0].JSON = json.RawMessage(`{
					"target": "target",
					"alignmentPeriod": "cloud-monitoring-auto"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+60s`, queries[0].Params["aggregation.alignmentPeriod"][0])
			})

			t.Run("and range is 23 hours", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].TimeRange.From = now.Add(-(time.Hour * 23))
				req.Queries[0].TimeRange.To = now
				req.Queries[0].JSON = json.RawMessage(`{
					"target": "target",
					"alignmentPeriod": "cloud-monitoring-auto"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+300s`, queries[0].Params["aggregation.alignmentPeriod"][0])
			})

			t.Run("and range is 7 days", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].TimeRange.From = now
				req.Queries[0].TimeRange.To = now.AddDate(0, 0, 7)
				req.Queries[0].JSON = json.RawMessage(`{
					"target": "target",
					"alignmentPeriod": "cloud-monitoring-auto"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+3600s`, queries[0].Params["aggregation.alignmentPeriod"][0])
			})
		})

		t.Run("and alignmentPeriod is set to stackdriver-auto", func(t *testing.T) { // legacy
			now := time.Now().UTC()

			t.Run("and range is two hours", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].TimeRange.From = now.Add(-(time.Hour * 2))
				req.Queries[0].TimeRange.To = now
				req.Queries[0].JSON = json.RawMessage(`{
					"target": "target",
					"alignmentPeriod": "stackdriver-auto"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+60s`, queries[0].Params["aggregation.alignmentPeriod"][0])

				// assign resource type to query parameters to be included in the deep link filter
				// in the actual workflow this information comes from the response of the Monitoring API
				queries[0].Params.Set("resourceType", "a/resource/type")
				dl := queries[0].buildDeepLink()

				expectedTimeSelection := map[string]string{
					"timeRange": "custom",
					"start":     req.Queries[0].TimeRange.From.Format(time.RFC3339),
					"end":       req.Queries[0].TimeRange.To.Format(time.RFC3339),
				}
				expectedTimeSeriesFilter := map[string]interface{}{
					"minAlignmentPeriod": `60s`,
				}
				verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
			})

			t.Run("and range is 22 hours", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].TimeRange.From = now.Add(-(time.Hour * 22))
				req.Queries[0].TimeRange.To = now
				req.Queries[0].JSON = json.RawMessage(`{
					"target": "target",
					"alignmentPeriod": "stackdriver-auto"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+60s`, queries[0].Params["aggregation.alignmentPeriod"][0])

				// assign resource type to query parameters to be included in the deep link filter
				// in the actual workflow this information comes from the response of the Monitoring API
				queries[0].Params.Set("resourceType", "a/resource/type")
				dl := queries[0].buildDeepLink()

				expectedTimeSelection := map[string]string{
					"timeRange": "custom",
					"start":     req.Queries[0].TimeRange.From.Format(time.RFC3339),
					"end":       req.Queries[0].TimeRange.To.Format(time.RFC3339),
				}
				expectedTimeSeriesFilter := map[string]interface{}{
					"minAlignmentPeriod": `60s`,
				}
				verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
			})

			t.Run("and range is 23 hours", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].TimeRange.From = now.Add(-(time.Hour * 23))
				req.Queries[0].TimeRange.To = now
				req.Queries[0].JSON = json.RawMessage(`{
					"target": "target",
					"alignmentPeriod": "stackdriver-auto"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+300s`, queries[0].Params["aggregation.alignmentPeriod"][0])

				// assign resource type to query parameters to be included in the deep link filter
				// in the actual workflow this information comes from the response of the Monitoring API
				queries[0].Params.Set("resourceType", "a/resource/type")
				dl := queries[0].buildDeepLink()

				expectedTimeSelection := map[string]string{
					"timeRange": "custom",
					"start":     req.Queries[0].TimeRange.From.Format(time.RFC3339),
					"end":       req.Queries[0].TimeRange.To.Format(time.RFC3339),
				}
				expectedTimeSeriesFilter := map[string]interface{}{
					"minAlignmentPeriod": `300s`,
				}
				verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
			})

			t.Run("and range is 7 days", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].TimeRange.From = now.AddDate(0, 0, -7)
				req.Queries[0].TimeRange.To = now
				req.Queries[0].JSON = json.RawMessage(`{
					"target": "target",
					"alignmentPeriod": "stackdriver-auto"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+3600s`, queries[0].Params["aggregation.alignmentPeriod"][0])

				// assign resource type to query parameters to be included in the deep link filter
				// in the actual workflow this information comes from the response of the Monitoring API
				queries[0].Params.Set("resourceType", "a/resource/type")
				dl := queries[0].buildDeepLink()

				expectedTimeSelection := map[string]string{
					"timeRange": "custom",
					"start":     req.Queries[0].TimeRange.From.Format(time.RFC3339),
					"end":       req.Queries[0].TimeRange.To.Format(time.RFC3339),
				}
				expectedTimeSeriesFilter := map[string]interface{}{
					"minAlignmentPeriod": `3600s`,
				}
				verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
			})
		})

		t.Run("and alignmentPeriod is set in frontend", func(t *testing.T) {
			t.Run("and alignment period is within accepted range", func(t *testing.T) {
				req := baseReq()
				req.Queries[0].Interval = 1000
				req.Queries[0].JSON = json.RawMessage(`{
					"alignmentPeriod": "+600s"
				}`)

				qes, err := service.buildQueryExecutors(slog, req)
				require.NoError(t, err)
				queries := getCloudMonitoringQueriesFromInterface(t, qes)
				assert.Equal(t, `+600s`, queries[0].Params["aggregation.alignmentPeriod"][0])

				// assign resource type to query parameters to be included in the deep link filter
				// in the actual workflow this information comes from the response of the Monitoring API
				queries[0].Params.Set("resourceType", "a/resource/type")
				dl := queries[0].buildDeepLink()

				expectedTimeSelection := map[string]string{
					"timeRange": "custom",
					"start":     "2018-03-15T13:00:00Z",
					"end":       "2018-03-15T13:34:00Z",
				}
				expectedTimeSeriesFilter := map[string]interface{}{
					"minAlignmentPeriod": `600s`,
				}
				verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
			})
		})

		t.Run("and query has aggregation mean set", func(t *testing.T) {
			req := baseReq()
			req.Queries[0].JSON = json.RawMessage(`{
				"metricType":         "a/metric/type",
				"crossSeriesReducer": "REDUCE_SUM",
				"view":               "FULL"
			}`)

			qes, err := service.buildQueryExecutors(slog, req)
			require.NoError(t, err)
			queries := getCloudMonitoringQueriesFromInterface(t, qes)

			assert.Equal(t, 1, len(queries))
			assert.Equal(t, "A", queries[0].RefID)
			assert.Equal(t, "aggregation.alignmentPeriod=%2B60s&aggregation.crossSeriesReducer=REDUCE_SUM&aggregation.perSeriesAligner=ALIGN_MEAN&filter=metric.type%3D%22a%2Fmetric%2Ftype%22&interval.endTime=2018-03-15T13%3A34%3A00Z&interval.startTime=2018-03-15T13%3A00%3A00Z&view=FULL", queries[0].Target)
			assert.Equal(t, 7, len(queries[0].Params))
			assert.Equal(t, "2018-03-15T13:00:00Z", queries[0].Params["interval.startTime"][0])
			assert.Equal(t, "2018-03-15T13:34:00Z", queries[0].Params["interval.endTime"][0])
			assert.Equal(t, "REDUCE_SUM", queries[0].Params["aggregation.crossSeriesReducer"][0])
			assert.Equal(t, "ALIGN_MEAN", queries[0].Params["aggregation.perSeriesAligner"][0])
			assert.Equal(t, "+60s", queries[0].Params["aggregation.alignmentPeriod"][0])
			assert.Equal(t, "metric.type=\"a/metric/type\"", queries[0].Params["filter"][0])
			assert.Equal(t, "FULL", queries[0].Params["view"][0])

			// assign resource type to query parameters to be included in the deep link filter
			// in the actual workflow this information comes from the response of the Monitoring API
			queries[0].Params.Set("resourceType", "a/resource/type")
			dl := queries[0].buildDeepLink()

			expectedTimeSelection := map[string]string{
				"timeRange": "custom",
				"start":     "2018-03-15T13:00:00Z",
				"end":       "2018-03-15T13:34:00Z",
			}
			expectedTimeSeriesFilter := map[string]interface{}{
				"minAlignmentPeriod": `60s`,
				"crossSeriesReducer": "REDUCE_SUM",
				"perSeriesAligner":   "ALIGN_MEAN",
				"filter":             "resource.type=\"a/resource/type\" metric.type=\"a/metric/type\"",
			}
			verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
		})

		t.Run("and query has group bys", func(t *testing.T) {
			req := baseReq()
			req.Queries[0].JSON = json.RawMessage(`{
				"metricType":         "a/metric/type",
				"crossSeriesReducer": "REDUCE_NONE",
				"groupBys":           ["metric.label.group1", "metric.label.group2"],
				"view":               "FULL"
			}`)

			qes, err := service.buildQueryExecutors(slog, req)
			require.NoError(t, err)
			queries := getCloudMonitoringQueriesFromInterface(t, qes)

			assert.Equal(t, 1, len(queries))
			assert.Equal(t, "A", queries[0].RefID)
			assert.Equal(t, "aggregation.alignmentPeriod=%2B60s&aggregation.crossSeriesReducer=REDUCE_NONE&aggregation.groupByFields=metric.label.group1&aggregation.groupByFields=metric.label.group2&aggregation.perSeriesAligner=ALIGN_MEAN&filter=metric.type%3D%22a%2Fmetric%2Ftype%22&interval.endTime=2018-03-15T13%3A34%3A00Z&interval.startTime=2018-03-15T13%3A00%3A00Z&view=FULL", queries[0].Target)
			assert.Equal(t, 8, len(queries[0].Params))
			assert.Equal(t, "2018-03-15T13:00:00Z", queries[0].Params["interval.startTime"][0])
			assert.Equal(t, "2018-03-15T13:34:00Z", queries[0].Params["interval.endTime"][0])
			assert.Equal(t, "ALIGN_MEAN", queries[0].Params["aggregation.perSeriesAligner"][0])
			assert.Equal(t, "metric.label.group1", queries[0].Params["aggregation.groupByFields"][0])
			assert.Equal(t, "metric.label.group2", queries[0].Params["aggregation.groupByFields"][1])
			assert.Equal(t, "metric.type=\"a/metric/type\"", queries[0].Params["filter"][0])
			assert.Equal(t, "FULL", queries[0].Params["view"][0])

			// assign resource type to query parameters to be included in the deep link filter
			// in the actual workflow this information comes from the response of the Monitoring API
			queries[0].Params.Set("resourceType", "a/resource/type")
			dl := queries[0].buildDeepLink()

			expectedTimeSelection := map[string]string{
				"timeRange": "custom",
				"start":     "2018-03-15T13:00:00Z",
				"end":       "2018-03-15T13:34:00Z",
			}
			expectedTimeSeriesFilter := map[string]interface{}{
				"minAlignmentPeriod": `60s`,
				"perSeriesAligner":   "ALIGN_MEAN",
				"filter":             "resource.type=\"a/resource/type\" metric.type=\"a/metric/type\"",
				"groupByFields":      []interface{}{"metric.label.group1", "metric.label.group2"},
			}
			verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)
		})
	})

	t.Run("Parse queries from frontend and build Google Cloud Monitoring API queries", func(t *testing.T) {
		fromStart := time.Date(2018, 3, 15, 13, 0, 0, 0, time.UTC).In(time.Local)
		req := &backend.QueryDataRequest{
			Queries: []backend.DataQuery{
				{
					RefID: "A",
					TimeRange: backend.TimeRange{
						From: fromStart,
						To:   fromStart.Add(34 * time.Minute),
					},
					JSON: json.RawMessage(`{
		 				 "queryType": "metrics",
                         "metricQuery": {
		 					"metricType": "a/metric/type",
		 					"view":       "FULL",
		 					"aliasBy":    "testalias",
		 					"type":       "timeSeriesQuery",
		 					"groupBys":   ["metric.label.group1", "metric.label.group2"]
		 				}
		 			}`),
				},
			},
		}
		t.Run("and query type is metrics", func(t *testing.T) {
			qes, err := service.buildQueryExecutors(slog, req)
			require.NoError(t, err)
			queries := getCloudMonitoringQueriesFromInterface(t, qes)

			assert.Equal(t, 1, len(queries))
			assert.Equal(t, "A", queries[0].RefID)
			assert.Equal(t, "aggregation.alignmentPeriod=%2B60s&aggregation.crossSeriesReducer=REDUCE_NONE&aggregation.groupByFields=metric.label.group1&aggregation.groupByFields=metric.label.group2&aggregation.perSeriesAligner=ALIGN_MEAN&filter=metric.type%3D%22a%2Fmetric%2Ftype%22&interval.endTime=2018-03-15T13%3A34%3A00Z&interval.startTime=2018-03-15T13%3A00%3A00Z&view=FULL", queries[0].Target)
			assert.Equal(t, 8, len(queries[0].Params))
			assert.Equal(t, "metric.label.group1", queries[0].Params["aggregation.groupByFields"][0])
			assert.Equal(t, "metric.label.group2", queries[0].Params["aggregation.groupByFields"][1])
			assert.Equal(t, "2018-03-15T13:00:00Z", queries[0].Params["interval.startTime"][0])
			assert.Equal(t, "2018-03-15T13:34:00Z", queries[0].Params["interval.endTime"][0])
			assert.Equal(t, "ALIGN_MEAN", queries[0].Params["aggregation.perSeriesAligner"][0])
			assert.Equal(t, "metric.type=\"a/metric/type\"", queries[0].Params["filter"][0])
			assert.Equal(t, "FULL", queries[0].Params["view"][0])
			assert.Equal(t, "testalias", queries[0].AliasBy)
			assert.Equal(t, []string{"metric.label.group1", "metric.label.group2"}, queries[0].GroupBys)

			// assign resource type to query parameters to be included in the deep link filter
			// in the actual workflow this information comes from the response of the Monitoring API
			queries[0].Params.Set("resourceType", "a/resource/type")
			dl := queries[0].buildDeepLink()

			expectedTimeSelection := map[string]string{
				"timeRange": "custom",
				"start":     "2018-03-15T13:00:00Z",
				"end":       "2018-03-15T13:34:00Z",
			}
			expectedTimeSeriesFilter := map[string]interface{}{
				"minAlignmentPeriod": `60s`,
				"perSeriesAligner":   "ALIGN_MEAN",
				"filter":             "resource.type=\"a/resource/type\" metric.type=\"a/metric/type\"",
				"groupByFields":      []interface{}{"metric.label.group1", "metric.label.group2"},
			}
			verifyDeepLink(t, dl, expectedTimeSelection, expectedTimeSeriesFilter)

			req.Queries[0].JSON = json.RawMessage(`{
				"queryType": "metrics",
				 "metricQuery": {
					"editorMode":  "mql",
					"projectName": "test-proj",
					"query":       "test-query",
					"aliasBy":     "test-alias"
				},
				"sloQuery": {}
			}`)

			qes, err = service.buildQueryExecutors(slog, req)
			require.NoError(t, err)
			tqueries := make([]*cloudMonitoringTimeSeriesQuery, 0)
			for _, qi := range qes {
				q, ok := qi.(*cloudMonitoringTimeSeriesQuery)
				assert.True(t, ok)
				tqueries = append(tqueries, q)
			}

			assert.Equal(t, 1, len(tqueries))
			assert.Equal(t, "A", tqueries[0].RefID)
			assert.Equal(t, "test-proj", tqueries[0].ProjectName)
			assert.Equal(t, "test-query", tqueries[0].Query)
			assert.Equal(t, "test-alias", tqueries[0].AliasBy)
		})

		t.Run("and query type is SLOs", func(t *testing.T) {
			req.Queries[0].JSON = json.RawMessage(`{
				"queryType": "slo",
				 "sloQuery": {
					"projectName":      "test-proj",
					"alignmentPeriod":  "stackdriver-auto",
					"perSeriesAligner": "ALIGN_NEXT_OLDER",
					"aliasBy":          "",
					"selectorName":     "select_slo_health",
					"serviceId":        "test-service",
					"sloId":            "test-slo"
				},
				"metricQuery": {}
			}`)

			qes, err := service.buildQueryExecutors(slog, req)
			require.NoError(t, err)
			queries := getCloudMonitoringQueriesFromInterface(t, qes)

			assert.Equal(t, 1, len(queries))
			assert.Equal(t, "A", queries[0].RefID)
			assert.Equal(t, "2018-03-15T13:00:00Z", queries[0].Params["interval.startTime"][0])
			assert.Equal(t, "2018-03-15T13:34:00Z", queries[0].Params["interval.endTime"][0])
			assert.Equal(t, `+60s`, queries[0].Params["aggregation.alignmentPeriod"][0])
			assert.Equal(t, "", queries[0].AliasBy)
			assert.Equal(t, "ALIGN_MEAN", queries[0].Params["aggregation.perSeriesAligner"][0])
			assert.Equal(t, `aggregation.alignmentPeriod=%2B60s&aggregation.perSeriesAligner=ALIGN_MEAN&filter=select_slo_health%28%22projects%2Ftest-proj%2Fservices%2Ftest-service%2FserviceLevelObjectives%2Ftest-slo%22%29&interval.endTime=2018-03-15T13%3A34%3A00Z&interval.startTime=2018-03-15T13%3A00%3A00Z`, queries[0].Target)
			assert.Equal(t, 5, len(queries[0].Params))

			req.Queries[0].JSON = json.RawMessage(`{
				"queryType": "slo",
				 "sloQuery": {
					"projectName":      "test-proj",
					"alignmentPeriod":  "stackdriver-auto",
					"perSeriesAligner": "ALIGN_NEXT_OLDER",
					"aliasBy":          "",
					"selectorName":     "select_slo_compliance",
					"serviceId":        "test-service",
					"sloId":            "test-slo"
				},
				"metricQuery": {}
			}`)

			qes, err = service.buildQueryExecutors(slog, req)
			require.NoError(t, err)
			qqueries := getCloudMonitoringQueriesFromInterface(t, qes)
			assert.Equal(t, "ALIGN_NEXT_OLDER", qqueries[0].Params["aggregation.perSeriesAligner"][0])

			dl := qqueries[0].buildDeepLink()
			assert.Empty(t, dl)

			req.Queries[0].JSON = json.RawMessage(`{
				"queryType": "slo",
				 "sloQuery": {
					"projectName":      "test-proj",
					"alignmentPeriod":  "stackdriver-auto",
					"perSeriesAligner": "ALIGN_NEXT_OLDER",
					"aliasBy":          "",
					"selectorName":     "select_slo_burn_rate",
					"serviceId":        "test-service",
					"sloId":            "test-slo",
					"lookbackPeriod":   "1h"
				},
				"metricQuery": {}
			}`)

			qes, err = service.buildQueryExecutors(slog, req)
			require.NoError(t, err)
			qqqueries := getCloudMonitoringQueriesFromInterface(t, qes)
			assert.Equal(t, `aggregation.alignmentPeriod=%2B60s&aggregation.perSeriesAligner=ALIGN_NEXT_OLDER&filter=select_slo_burn_rate%28%22projects%2Ftest-proj%2Fservices%2Ftest-service%2FserviceLevelObjectives%2Ftest-slo%22%2C+%221h%22%29&interval.endTime=2018-03-15T13%3A34%3A00Z&interval.startTime=2018-03-15T13%3A00%3A00Z`, qqqueries[0].Target)
		})
	})

	t.Run("when interpolating filter wildcards", func(t *testing.T) {
		t.Run("and wildcard is used in the beginning and the end of the word", func(t *testing.T) {
			t.Run("and there's no wildcard in the middle of the word", func(t *testing.T) {
				value := interpolateFilterWildcards("*-central1*")
				assert.Equal(t, `has_substring("-central1")`, value)
			})
			t.Run("and there is a wildcard in the middle of the word", func(t *testing.T) {
				value := interpolateFilterWildcards("*-cent*ral1*")
				assert.False(t, strings.HasPrefix(value, `has_substring`))
			})
		})

		t.Run("and wildcard is used in the beginning of the word", func(t *testing.T) {
			t.Run("and there is not a wildcard elsewhere in the word", func(t *testing.T) {
				value := interpolateFilterWildcards("*-central1")
				assert.Equal(t, `ends_with("-central1")`, value)
			})
			t.Run("and there is a wildcard elsewhere in the word", func(t *testing.T) {
				value := interpolateFilterWildcards("*-cent*al1")
				assert.False(t, strings.HasPrefix(value, `ends_with`))
			})
		})

		t.Run("and wildcard is used at the end of the word", func(t *testing.T) {
			t.Run("and there is not a wildcard elsewhere in the word", func(t *testing.T) {
				value := interpolateFilterWildcards("us-central*")
				assert.Equal(t, `starts_with("us-central")`, value)
			})
			t.Run("and there is a wildcard elsewhere in the word", func(t *testing.T) {
				value := interpolateFilterWildcards("*us-central*")
				assert.False(t, strings.HasPrefix(value, `starts_with`))
			})
		})

		t.Run("and wildcard is used in the middle of the word", func(t *testing.T) {
			t.Run("and there is only one wildcard", func(t *testing.T) {
				value := interpolateFilterWildcards("us-ce*tral1-b")
				assert.Equal(t, `monitoring.regex.full_match("^us\\-ce.*tral1\\-b$")`, value)
			})

			t.Run("and there is more than one wildcard", func(t *testing.T) {
				value := interpolateFilterWildcards("us-ce*tra*1-b")
				assert.Equal(t, `monitoring.regex.full_match("^us\\-ce.*tra.*1\\-b$")`, value)
			})
		})

		t.Run("and wildcard is used in the middle of the word and in the beginning of the word", func(t *testing.T) {
			value := interpolateFilterWildcards("*s-ce*tral1-b")
			assert.Equal(t, `monitoring.regex.full_match("^.*s\\-ce.*tral1\\-b$")`, value)
		})

		t.Run("and wildcard is used in the middle of the word and in the ending of the word", func(t *testing.T) {
			value := interpolateFilterWildcards("us-ce*tral1-*")
			assert.Equal(t, `monitoring.regex.full_match("^us\\-ce.*tral1\\-.*$")`, value)
		})

		t.Run("and no wildcard is used", func(t *testing.T) {
			value := interpolateFilterWildcards("us-central1-a}")
			assert.Equal(t, `us-central1-a}`, value)
		})
	})

	t.Run("when building filter string", func(t *testing.T) {
		t.Run("and there's no regex operator", func(t *testing.T) {
			t.Run("and there are wildcards in a filter value", func(t *testing.T) {
				filterParts := []string{"zone", "=", "*-central1*"}
				value := buildFilterString("somemetrictype", filterParts)
				assert.Equal(t, `metric.type="somemetrictype" zone=has_substring("-central1")`, value)
			})

			t.Run("and there are no wildcards in any filter value", func(t *testing.T) {
				filterParts := []string{"zone", "!=", "us-central1-a"}
				value := buildFilterString("somemetrictype", filterParts)
				assert.Equal(t, `metric.type="somemetrictype" zone!="us-central1-a"`, value)
			})
		})

		t.Run("and there is a regex operator", func(t *testing.T) {
			filterParts := []string{"zone", "=~", "us-central1-a~"}
			value := buildFilterString("somemetrictype", filterParts)
			assert.NotContains(t, value, `=~`)
			assert.Contains(t, value, `zone=`)

			assert.Contains(t, value, `zone=monitoring.regex.full_match("us-central1-a~")`)
		})
	})

	t.Run("and query preprocessor is not defined", func(t *testing.T) {
		req := baseReq()
		req.Queries[0].JSON = json.RawMessage(`{
			"metricType":         "a/metric/type",
			"crossSeriesReducer": "REDUCE_MIN",
			"perSeriesAligner":   "REDUCE_SUM",
			"alignmentPeriod":    "+60s",
			"groupBys":           ["labelname"],
			"view":               "FULL"
		}`)

		qes, err := service.buildQueryExecutors(slog, req)
		require.NoError(t, err)
		queries := getCloudMonitoringQueriesFromInterface(t, qes)

		assert.Equal(t, 1, len(queries))
		assert.Equal(t, "REDUCE_MIN", queries[0].Params["aggregation.crossSeriesReducer"][0])
		assert.Equal(t, "REDUCE_SUM", queries[0].Params["aggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["aggregation.alignmentPeriod"][0])
		assert.Equal(t, "labelname", queries[0].Params["aggregation.groupByFields"][0])

		assert.NotContains(t, queries[0].Params, "secondaryAggregation.crossSeriesReducer")
		assert.NotContains(t, "REDUCE_SUM", queries[0].Params, "secondaryAggregation.perSeriesAligner")
		assert.NotContains(t, "+60s", queries[0].Params, "secondaryAggregation.alignmentPeriod")
		assert.NotContains(t, "labelname", queries[0].Params, "secondaryAggregation.groupByFields")
	})

	t.Run("and query preprocessor is set to none", func(t *testing.T) {
		req := baseReq()
		req.Queries[0].JSON = json.RawMessage(`{
			"metricType":         "a/metric/type",
			"crossSeriesReducer": "REDUCE_MIN",
			"perSeriesAligner":   "REDUCE_SUM",
			"alignmentPeriod":    "+60s",
			"groupBys":           ["labelname"],
			"view":               "FULL",
			"preprocessor":       "none"
		}`)

		qes, err := service.buildQueryExecutors(slog, req)
		require.NoError(t, err)
		queries := getCloudMonitoringQueriesFromInterface(t, qes)

		assert.Equal(t, 1, len(queries))
		assert.Equal(t, "REDUCE_MIN", queries[0].Params["aggregation.crossSeriesReducer"][0])
		assert.Equal(t, "REDUCE_SUM", queries[0].Params["aggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["aggregation.alignmentPeriod"][0])
		assert.Equal(t, "labelname", queries[0].Params["aggregation.groupByFields"][0])

		assert.NotContains(t, queries[0].Params, "secondaryAggregation.crossSeriesReducer")
		assert.NotContains(t, "REDUCE_SUM", queries[0].Params, "secondaryAggregation.perSeriesAligner")
		assert.NotContains(t, "+60s", queries[0].Params, "secondaryAggregation.alignmentPeriod")
		assert.NotContains(t, "labelname", queries[0].Params, "secondaryAggregation.groupByFields")
	})

	t.Run("and query preprocessor is set to rate and there's no group bys", func(t *testing.T) {
		req := baseReq()
		req.Queries[0].JSON = json.RawMessage(`{
			"metricType":         "a/metric/type",
			"crossSeriesReducer": "REDUCE_SUM",
			"perSeriesAligner":   "REDUCE_MIN",
			"alignmentPeriod":    "+60s",
			"groupBys":           [],
			"view":               "FULL",
			"preprocessor":       "rate"
		}`)

		qes, err := service.buildQueryExecutors(slog, req)
		require.NoError(t, err)
		queries := getCloudMonitoringQueriesFromInterface(t, qes)

		assert.Equal(t, 1, len(queries))
		assert.Equal(t, "REDUCE_NONE", queries[0].Params["aggregation.crossSeriesReducer"][0])
		assert.Equal(t, "ALIGN_RATE", queries[0].Params["aggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["aggregation.alignmentPeriod"][0])

		assert.Equal(t, "REDUCE_SUM", queries[0].Params["secondaryAggregation.crossSeriesReducer"][0])
		assert.Equal(t, "REDUCE_MIN", queries[0].Params["secondaryAggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["secondaryAggregation.alignmentPeriod"][0])
	})

	t.Run("and query preprocessor is set to rate and group bys exist", func(t *testing.T) {
		req := baseReq()
		req.Queries[0].JSON = json.RawMessage(`{
			"metricType":         "a/metric/type",
			"crossSeriesReducer": "REDUCE_SUM",
			"perSeriesAligner":   "REDUCE_MIN",
			"alignmentPeriod":    "+60s",
			"groupBys":           ["labelname"],
			"view":               "FULL",
			"preprocessor":       "rate"
		}`)

		qes, err := service.buildQueryExecutors(slog, req)
		require.NoError(t, err)
		queries := getCloudMonitoringQueriesFromInterface(t, qes)

		assert.Equal(t, 1, len(queries))
		assert.Equal(t, "REDUCE_SUM", queries[0].Params["aggregation.crossSeriesReducer"][0])
		assert.Equal(t, "ALIGN_RATE", queries[0].Params["aggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["aggregation.alignmentPeriod"][0])
		assert.Equal(t, "labelname", queries[0].Params["aggregation.groupByFields"][0])

		assert.Equal(t, "REDUCE_SUM", queries[0].Params["secondaryAggregation.crossSeriesReducer"][0])
		assert.Equal(t, "REDUCE_MIN", queries[0].Params["secondaryAggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["secondaryAggregation.alignmentPeriod"][0])
		assert.Equal(t, "labelname", queries[0].Params["secondaryAggregation.groupByFields"][0])
	})

	t.Run("and query preprocessor is set to delta and there's no group bys", func(t *testing.T) {
		req := baseReq()
		req.Queries[0].JSON = json.RawMessage(`{
			"metricType":         "a/metric/type",
			"crossSeriesReducer": "REDUCE_MIN",
			"perSeriesAligner":   "REDUCE_SUM",
			"alignmentPeriod":    "+60s",
			"groupBys":           [],
			"view":               "FULL",
			"preprocessor":       "delta"
		}`)

		qes, err := service.buildQueryExecutors(slog, req)
		require.NoError(t, err)
		queries := getCloudMonitoringQueriesFromInterface(t, qes)

		assert.Equal(t, 1, len(queries))
		assert.Equal(t, "REDUCE_NONE", queries[0].Params["aggregation.crossSeriesReducer"][0])
		assert.Equal(t, "ALIGN_DELTA", queries[0].Params["aggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["aggregation.alignmentPeriod"][0])

		assert.Equal(t, "REDUCE_MIN", queries[0].Params["secondaryAggregation.crossSeriesReducer"][0])
		assert.Equal(t, "REDUCE_SUM", queries[0].Params["secondaryAggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["secondaryAggregation.alignmentPeriod"][0])
	})

	t.Run("and query preprocessor is set to delta and group bys exist", func(t *testing.T) {
		req := baseReq()
		req.Queries[0].JSON = json.RawMessage(`{
			"metricType":         "a/metric/type",
			"crossSeriesReducer": "REDUCE_MIN",
			"perSeriesAligner":   "REDUCE_SUM",
			"alignmentPeriod":    "+60s",
			"groupBys":           ["labelname"],
			"view":               "FULL",
			"preprocessor":       "delta"
		}`)

		qes, err := service.buildQueryExecutors(slog, req)
		require.NoError(t, err)
		queries := getCloudMonitoringQueriesFromInterface(t, qes)

		assert.Equal(t, 1, len(queries))
		assert.Equal(t, "REDUCE_MIN", queries[0].Params["aggregation.crossSeriesReducer"][0])
		assert.Equal(t, "ALIGN_DELTA", queries[0].Params["aggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["aggregation.alignmentPeriod"][0])
		assert.Equal(t, "labelname", queries[0].Params["aggregation.groupByFields"][0])

		assert.Equal(t, "REDUCE_MIN", queries[0].Params["secondaryAggregation.crossSeriesReducer"][0])
		assert.Equal(t, "REDUCE_SUM", queries[0].Params["secondaryAggregation.perSeriesAligner"][0])
		assert.Equal(t, "+60s", queries[0].Params["secondaryAggregation.alignmentPeriod"][0])
		assert.Equal(t, "labelname", queries[0].Params["secondaryAggregation.groupByFields"][0])
	})
}

func getCloudMonitoringQueriesFromInterface(t *testing.T, qes []cloudMonitoringQueryExecutor) []*cloudMonitoringTimeSeriesFilter {
	t.Helper()

	queries := make([]*cloudMonitoringTimeSeriesFilter, 0)
	for _, qi := range qes {
		q, ok := qi.(*cloudMonitoringTimeSeriesFilter)
		require.Truef(t, ok, "Received wrong type %T", qi)
		queries = append(queries, q)
	}
	return queries
}

func verifyDeepLink(t *testing.T, dl string, expectedTimeSelection map[string]string,
	expectedTimeSeriesFilter map[string]interface{}) {
	t.Helper()

	u, err := url.Parse(dl)
	require.NoError(t, err)
	assert.Equal(t, "https", u.Scheme)
	assert.Equal(t, "accounts.google.com", u.Host)
	assert.Equal(t, "/AccountChooser", u.Path)

	params, err := url.ParseQuery(u.RawQuery)
	require.NoError(t, err)

	continueParam := params.Get("continue")
	assert.NotEmpty(t, continueParam)

	u, err = url.Parse(continueParam)
	require.NoError(t, err)

	params, err = url.ParseQuery(u.RawQuery)
	require.NoError(t, err)

	deepLinkParam := params.Get("Grafana_deeplink")
	assert.NotEmpty(t, deepLinkParam)

	pageStateStr := params.Get("pageState")
	assert.NotEmpty(t, pageStateStr)

	var pageState map[string]map[string]interface{}
	err = json.Unmarshal([]byte(pageStateStr), &pageState)
	require.NoError(t, err)

	timeSelection, ok := pageState["timeSelection"]
	assert.True(t, ok)
	for k, v := range expectedTimeSelection {
		s, ok := timeSelection[k].(string)
		assert.True(t, ok)
		assert.Equal(t, v, s)
	}

	dataSets, ok := pageState["xyChart"]["dataSets"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 1, len(dataSets))
	dataSet, ok := dataSets[0].(map[string]interface{})
	assert.True(t, ok)
	i, ok := dataSet["timeSeriesFilter"]
	assert.True(t, ok)
	timeSeriesFilter := i.(map[string]interface{})
	for k, v := range expectedTimeSeriesFilter {
		s, ok := timeSeriesFilter[k]
		assert.True(t, ok)
		rt := reflect.TypeOf(v)
		switch rt.Kind() {
		case reflect.Slice, reflect.Array:
			assert.Equal(t, v, s)
		default:
			assert.Equal(t, v, s)
		}
	}
}

func baseReq() *backend.QueryDataRequest {
	fromStart := time.Date(2018, 3, 15, 13, 0, 0, 0, time.UTC).In(time.Local)
	query := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				TimeRange: backend.TimeRange{
					From: fromStart,
					To:   fromStart.Add(34 * time.Minute),
				},
				JSON: json.RawMessage(`{
					"metricType": "a/metric/type",
					"view":       "FULL",
					"aliasBy":    "testalias",
					"type":       "timeSeriesQuery"
				}`),
			},
		},
	}
	return query
}

func TestCheckHealth(t *testing.T) {
	t.Run("and using GCE authentation should return proper error", func(t *testing.T) {
		im := datasource.NewInstanceManager(func(s backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
			return &datasourceInfo{
				authenticationType: gceAuthentication,
			}, nil
		})
		service := &Service{
			im: im,
			gceDefaultProjectGetter: func(ctx context.Context) (string, error) {
				return "", fmt.Errorf("not found!")
			},
		}
		res, err := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{
			PluginContext: backend.PluginContext{
				DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{},
			},
		})
		assert.Nil(t, err)
		assert.Equal(t, &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "not found!",
		}, res)
	})
}
