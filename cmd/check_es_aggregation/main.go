package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
	elastic "gopkg.in/olivere/elastic.v5"

	"github.com/olorin/nagiosplugin"
)

var (
	config    *args
	logger    *log.Logger
	warnRange *nagiosplugin.Range
	critRange *nagiosplugin.Range
)

type args struct {
	elasticsearchURL  *string
	debug             *bool
	query             *string
	pTerm             *[]string
	nTerm             *[]string
	pRange            *string
	nRange            *string
	index             *string
	key               *string
	desc              *string
	agg               *string
	pct               *float64
	unit              *string
	duration          *time.Duration
	warningThreshold  *string
	criticalThreshold *string
	nullCode          *int
	verbose           *bool
}

func (a *args) floatCrit() float64 {
	var n float64
	n, err := strconv.ParseFloat(*a.criticalThreshold, 64)
	if err != nil {
		panic(err)
	}
	return n
}

func (a *args) floatWarn() float64 {
	var n float64
	n, err := strconv.ParseFloat(*a.warningThreshold, 64)
	if err != nil {
		panic(err)
	}
	return n
}

/*
 * Searcher
 *
 */

type searcher struct {
	idx     string
	es      *elastic.Client
	agg     *elastic.DateRangeAggregation
	aggName string
	pctVal  string
	qry     *elastic.BoolQuery
	res     *elastic.SearchResult
}

func newSearcher(url string, idx string, timeAgo time.Duration, logger *log.Logger) (*searcher, error) {
	var err error
	var client *elastic.Client
	if logger != nil {
		client, err = elastic.NewClient(elastic.SetURL(url), elastic.SetTraceLog(logger))
	} else {
		client, err = elastic.NewClient(elastic.SetURL(url))
	}
	if err != nil {
		return nil, err
	}

	s := &searcher{es: client, idx: idx}

	now := time.Now()
	from := now.Add(-timeAgo)

	s.agg = elastic.NewDateRangeAggregation().Field("@timestamp").Between(from, now)
	s.qry = elastic.NewBoolQuery()

	return s, nil
}

func (s *searcher) AddTermFilter(field string, value string, negative bool) *searcher {
	if negative {
		s.qry = s.qry.MustNot(elastic.NewTermQuery(field, value))
	} else {
		s.qry = s.qry.Must(elastic.NewTermQuery(field, value))
	}
	return s
}

func (s *searcher) AddRangeFilter(field string, rng string, negative bool) *searcher {
	q := elastic.NewRangeQuery("range_" + field)
	switch {
	case strings.HasPrefix(rng, ">"):
		q = q.Gt(strings.TrimPrefix(rng, ">"))
	case strings.HasPrefix(rng, ">="):
		q = q.Gte(strings.TrimPrefix(rng, ">="))
	case strings.HasPrefix(rng, "<"):
		q = q.Lt(strings.TrimPrefix(rng, "<"))
	case strings.HasPrefix(rng, "<="):
		q = q.Lte(strings.TrimPrefix(rng, "<="))
	case strings.Contains(rng, " TO "):
		r := strings.Split(rng, " TO ")
		q = q.From(r[0]).To(r[1])
	}
	if negative {
		s.qry = s.qry.MustNot(q)
	} else {
		s.qry = s.qry.Must(q)
	}
	return s
}

func (s *searcher) AddSubAggregation(field string, name string, params ...interface{}) *searcher {
	var agg elastic.Aggregation

	switch name {
	case "min":
		agg = elastic.NewMinAggregation().Field(field)
	case "max":
		agg = elastic.NewMaxAggregation().Field(field)
	case "avg":
		agg = elastic.NewAvgAggregation().Field(field)
	case "sum":
		agg = elastic.NewSumAggregation().Field(field)
	case "pct":
		pctFloat, _ := params[0].(float64)
		s.pctVal = strconv.FormatFloat(pctFloat, 'f', 1, 64)
		agg = elastic.NewPercentilesAggregation().Field(field).Percentiles(pctFloat)
	case "pctr":
		pctFloat, _ := params[0].(float64)
		s.pctVal = strconv.FormatFloat(pctFloat, 'f', 1, 64)
		agg = elastic.NewPercentileRanksAggregation().Field(field).Values(pctFloat)
	case "stdev", "stdevmin", "stdevmax", "var":
		agg = elastic.NewExtendedStatsAggregation().Field(field)
	default:
		return s
	}

	s.aggName = name + "_" + field
	s.agg = s.agg.SubAggregation(s.aggName, agg)

	return s
}

func (s *searcher) Result() (float64, error) {
	if s.res.TotalHits() == int64(0) {
		return float64(0), &NoSearchResultError{"0 hits"}
	}
	aggr, ok := s.res.Aggregations.DateRange("aggr")
	if !ok {
		return float64(0), &NoSearchResultError{"no aggregations"}
	}
	if len(aggr.Buckets) == 0 {
		return float64(0), &NoSearchResultError{"0 aggregation buckets"}
	}
	var val float64
	switch *config.agg {
	case "min":
		stat, ok := aggr.Buckets[0].Min(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = *stat.Value
	case "max":
		stat, ok := aggr.Buckets[0].Max(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = *stat.Value
	case "avg":
		stat, ok := aggr.Buckets[0].Avg(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = *stat.Value
	case "sum":
		stat, ok := aggr.Buckets[0].Sum(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = *stat.Value
	case "pct":
		stat, ok := aggr.Buckets[0].Percentiles(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = stat.Values[s.pctVal]
	case "pctr":
		stat, ok := aggr.Buckets[0].PercentileRanks(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = stat.Values[s.pctVal]
	case "stdev":
		stat, ok := aggr.Buckets[0].ExtendedStats(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = *stat.StdDeviation
	case "stdevmin":
		stat, ok := aggr.Buckets[0].ExtendedStats(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = *stat.Min
	case "stdevmax":
		stat, ok := aggr.Buckets[0].ExtendedStats(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = *stat.Max
	case "var":
		stat, ok := aggr.Buckets[0].ExtendedStats(s.aggName)
		if !ok {
			return float64(0), &NoAggrValuesError{*config.agg}
		}
		val = *stat.Variance
	default:
		return float64(0), &NoAggrValuesError{*config.agg}
	}
	return val, nil
}

func (s *searcher) Search() error {
	var err error
	s.res, err = s.es.Search(s.idx).Query(s.qry).Aggregation("aggr", s.agg).Do(context.Background())
	return err
}

// ----------------------------------------------------------------------------------

type Error struct {
	msg string
}

type NoSearchResultError Error
type NoAggrValuesError Error

func (e *NoSearchResultError) Error() string {
	return fmt.Sprintf("No data in search result - %s", e.msg)
}

func (e *NoAggrValuesError) Error() string {
	return fmt.Sprintf("Aggregation result value missing in response - %s (see --debug)", e.msg)
}

// ----------------------------------------------------------------------------------

func validateTreshold(in string) (*nagiosplugin.Range, error) {
	rng, err := nagiosplugin.ParseRange(in)
	if err != nil {
		return nil, err
	}
	return rng, nil
}

func fields(in string) []string {
	f := strings.FieldsFunc(in,
		func(r rune) bool {
			return strings.ContainsRune(":=", r)
		},
	)
	return f
}

func filter(s *searcher) {
	for _, term := range *config.pTerm {
		f := fields(term)
		s.AddTermFilter(f[0], f[1], false)
	}
	for _, term := range *config.nTerm {
		f := fields(term)
		s.AddTermFilter(f[0], f[1], true)
	}
	if *config.pRange != "" {
		f := fields(*config.pRange)
		s.AddRangeFilter(f[0], f[1], false)
	}
	if *config.nRange != "" {
		f := fields(*config.pRange)
		s.AddRangeFilter(f[0], f[1], true)
	}
}

func aggregate(s *searcher) {
	var params interface{}
	if *config.agg == "pct" {
		params = *config.pct
	}
	s.AddSubAggregation(*config.key, *config.agg, params)
}

// ----------------------------------------------------------------------------------

func init() {
	var err error

	config = &args{}

	template := kingpin.SeparateOptionalFlagsUsageTemplate + `

Supported aggregations:
  min          Minimum value
  max          Maximum value
  avg          Average value
  sum          Sum of all values
  pct          N-th percentile value (optional -p argument)
  pctr         Percentile rank of a value (uses -p argument)
  stdev        Standard deviation
  stdevmin     Standard deviation lower boundary
  stdevmax     Standard deviation upper boundary
  var          Variance

Notes:
  When filtering by terms, you might need to use the '<field>.raw:<value>'
  representation of the field to match the exact string.
`

	params := kingpin.New("check-es-aggregation", "Nagios Plugin to compute ElasticSearch aggregations").UsageTemplate(template)
	config.elasticsearchURL = params.Flag("es-url", "Elasticsearch URL.").Short('e').Default("http://localhost:9200").String()
	config.debug = params.Flag("debug", "Enable logging of HTTP requests to STDERR").Bool()
	config.index = params.Flag("index-pattern", "Elasticsearch index pattern, eg. logstash-*").Default("logstash-*").String()
	config.key = params.Flag("key", "Elasticsearch document key to aggregate (check result will be based on the value of this field)").Short('k').Required().String()
	config.query = params.Flag("query", "Elasticsearch query string").Short('q').Default("*").String()
	config.pTerm = params.Flag("term", "Elasticsearch positive filter").Short('t').Strings()
	config.nTerm = params.Flag("not-term", "Elasticsearch negative filter").Strings()
	config.pRange = params.Flag("range", "Elasticsearch value positive range filter").String()
	config.nRange = params.Flag("not-range", "Elasticsearch value negative range filter").String()
	config.agg = params.Flag("aggregation", "Elasticsearch aggregation to compute").Short('a').Default("max").String()
	config.pct = params.Flag("percentile", "Elasticsearch percentile aggregations parameter").Short('p').Default("99.0").Float64()
	config.unit = params.Flag("unit", "Unit displayed in the check description").Short('u').String()
	config.desc = params.Flag("desc", "Check description").Short('d').Required().String()
	config.duration = params.Flag("duration", "Time range to perform the search on.").Default("5m").Duration()
	config.warningThreshold = params.Flag("warning", "Warning threshold number").Short('w').Required().String()
	config.criticalThreshold = params.Flag("critical", "Critical threshold number").Short('c').Required().String()
	config.nullCode = params.Flag("null-code", "zero search results fallback code").Short('n').Default("2").Int()
	config.verbose = params.Flag("verbose", "Increase verbosity for debugging").Bool()

	params.Parse(os.Args[1:])

	warnRange, err = validateTreshold(*config.warningThreshold)
	if err != nil {
		log.Fatal(err)
	}

	critRange, err = validateTreshold(*config.criticalThreshold)
	if err != nil {
		log.Fatal(err)
	}

	if *config.index == "" || *config.index == "*" {
		log.Fatalf("Invalid ES index '%s' given", *config.index)
	}

	if *config.debug {
		logger = log.New(os.Stderr, "check-es-aggregation", 0)
	}

}

func main() {
	var err error
	check := nagiosplugin.NewCheck()
	defer check.Finish() // If exit early or panic, still output a result.

	// initialize searcher
	searcher, err := newSearcher(*config.elasticsearchURL, *config.index, *config.duration, logger)
	if err != nil {
		check.AddResult(nagiosplugin.CRITICAL,
			fmt.Sprintf("Failed to connect to %v: %v", *config.elasticsearchURL, err))
		return
	}

	filter(searcher)
	aggregate(searcher)

	// do the search
	err = searcher.Search()
	if err != nil {
		check.AddResult(nagiosplugin.CRITICAL,
			fmt.Sprintf("Failed to execute search at %v, index %v: %v",
				*config.elasticsearchURL, *config.index, err))
		return
	}

	// handle the result
	value, err := searcher.Result()
	check.AddPerfDatum(*config.key, *config.unit, value, 0.0, math.Inf(1), config.floatWarn(), config.floatCrit())
	if err != nil {
		switch *config.nullCode {
		case 0:
			check.AddResultf(nagiosplugin.OK, "%s %f (%s)", *config.desc, value, err.Error())
		case 1:
			check.AddResultf(nagiosplugin.WARNING, "%s %f (%s)", *config.desc, value, err.Error())
		case 2:
			check.AddResultf(nagiosplugin.CRITICAL, "%s %f (%s)", *config.desc, value, err.Error())
		default:
			check.AddResultf(nagiosplugin.UNKNOWN, "%s %f (%s)", *config.desc, value, err.Error())
		}
		return
	}

	switch {
	case critRange.Check(value):
		check.AddResultf(nagiosplugin.CRITICAL, "%s %f > %s", *config.desc, value, *config.criticalThreshold)
	case warnRange.Check(value):
		check.AddResultf(nagiosplugin.WARNING, "%s %f > %s", *config.desc, value, *config.warningThreshold)
	default:
		check.AddResultf(nagiosplugin.OK, "%s %f", *config.desc, value)
	}
}
