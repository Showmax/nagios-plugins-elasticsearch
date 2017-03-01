package main

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
	"gopkg.in/olivere/elastic.v3"

	"github.com/olorin/nagiosplugin"
)

var (
	elasticsearchURL  = kingpin.Flag("elasticsearch.url", "Elasticsearch URL.").Short('e').Default("http://localhost:9200").String()
	query             = kingpin.Flag("query", "Elasticsearch query string").Short('q').Required().String()
	index             = kingpin.Flag("index-pattern", "Elasticsearch index pattern, eg. logstash-*").Required().String()
	key               = kingpin.Flag("key", "Elasticsearch document key to aggregate (check will be based on the value of this field).").Short('k').Required().String()
	desc              = kingpin.Flag("desc", "Check description").Short('d').Required().String()
	unit              = kingpin.Flag("unit", "Unit displayed in the check description").Short('u').String()
	minutes           = kingpin.Flag("minutes", "Time range to perform the search on.").Short('m').Default("5").Int()
	warningThreshold  = kingpin.Flag("warning", "Warning threshold number.").Short('w').Required().String()
	criticalThreshold = kingpin.Flag("critical", "Critical threshold number.").Short('c').Required().String()
	verbose           = kingpin.Flag("verbose", "Increase verbosity for debugging.").Bool()
)

func main() {
	kingpin.Parse()
	check := nagiosplugin.NewCheck()

	defer check.Finish() // If exit early or panic, still output a result.

	warning, err := strconv.ParseFloat(*warningThreshold, 64)
	if err != nil {
		panic(err)
	}

	critical, err := strconv.ParseFloat(*criticalThreshold, 64)
	if err != nil {
		panic(err)
	}

	if *index == "" || *index == "*" {
		panic(fmt.Errorf("Invalid ES index '%s' given", *index))
	}

	now := time.Now()
	from := now.Add(-(time.Duration(*minutes) * time.Minute))

	client, err := elastic.NewClient(elastic.SetURL(*elasticsearchURL))
	if err != nil {
		check.AddResult(nagiosplugin.CRITICAL,
			fmt.Sprintf("Failed to connect to %v: %v", *elasticsearchURL, err))
		return
	}

	timeRangeAgg := elastic.NewDateRangeAggregation().Field("@timestamp").Between(from, now)
	maxDurationAgg := elastic.NewMaxAggregation().Field(*key)
	timeRangeAgg = timeRangeAgg.SubAggregation("avgDuration", maxDurationAgg)

	//index := fmt.Sprintf("logs-app-varnishlog-%d.%02d.%02d", now.Year(), now.Month(), now.Day())
	searchResult, err := client.Search().
		Index(*index).
		Aggregation("timeRange", timeRangeAgg).
		Query(elastic.NewQueryStringQuery(*query)).
		Do()
	if err != nil {
		check.AddResult(nagiosplugin.CRITICAL,
			fmt.Sprintf("Failed to execute search at %v, index %v: %v",
				*elasticsearchURL, index, err))
		return
	}

	durationOverTime, found := searchResult.Aggregations.DateRange("timeRange")
	if !found {
		check.AddResult(nagiosplugin.CRITICAL, "Query has returned no results.")
		return
	}

	bucketCount := len(durationOverTime.Buckets)
	if bucketCount < 1 {
		check.AddResult(nagiosplugin.CRITICAL, "Result has no buckets.")
		return
	}

	var max *elastic.AggregationValueMetric
	max, found = durationOverTime.Buckets[0].Max("avgDuration")
	if !found || max == nil || max.Value == nil {
		check.AddResult(nagiosplugin.CRITICAL,
			"There is no value for max(avgDuration). Perhaps the Query didn't match anything!")
		return
	}
	avgDurationMs := *max.Value

	// Add an 'OK' result - if no 'worse' check results have been
	// added, this is the one that will be output.
	check.AddResult(nagiosplugin.OK, fmt.Sprintf("%s OK", *desc))

	check.AddPerfDatum(*key, *unit, avgDurationMs, 0.0, math.Inf(1), warning, critical)

	// Parse a range from the command line and warn on a match.
	warnRange, err := nagiosplugin.ParseRange(*warningThreshold)
	if err != nil {
		check.AddResult(nagiosplugin.UNKNOWN, "error parsing warning range")
		return
	}

	critRange, err := nagiosplugin.ParseRange(*criticalThreshold)
	if err != nil {
		check.AddResult(nagiosplugin.UNKNOWN, "error parsing critical range")
		return
	}

	if warnRange.Check(avgDurationMs) {
		check.AddResult(nagiosplugin.WARNING, fmt.Sprintf("%s %f above warning threshold", *desc, avgDurationMs))
		return
	}
	if critRange.Check(avgDurationMs) {
		check.AddResult(nagiosplugin.CRITICAL, fmt.Sprintf("%s %f above critical threshold", *desc, avgDurationMs))
		return
	}
}
