# ElasticSearch Nagios plugin
Generate alerts based on elasticsearch search query.

By providing and search query, a time range, a key (the key's value must be a number), this application will check the aggregated result from the key's values over the time range specified (5min by default) and return a nagios-compatible alert.

## Supported Aggregations
- Basic aggergations
  - ```min``` - [minumum value](https://www.elastic.co/guide/en/elasticsearch/reference/current/search-aggregations-metrics-min-aggregation.html)
  - ```max``` - [maximum value](https://www.elastic.co/guide/en/elasticsearch/reference/current/search-aggregations-metrics-max-aggregation.html)
  - ```avg``` - [average value](https://www.elastic.co/guide/en/elasticsearch/reference/current/search-aggregations-metrics-avg-aggregation.html)
  - ```sum``` - [sum of all values](https://www.elastic.co/guide/en/elasticsearch/reference/current/search-aggregations-metrics-sum-aggregation.html)
  - ```pct``` - [percentile](https://www.elastic.co/guide/en/elasticsearch/reference/current/search-aggregations-metrics-percentile-aggregation.html) (requires percent value, defaults to 99.0)
  - ```pctr``` - [percentile rank](https://www.elastic.co/guide/en/elasticsearch/reference/current/search-aggregations-metrics-percentile-rank-aggregation.html) (requires value for `p`, defaults to 99.0)
- [Extended aggregations](https://www.elastic.co/guide/en/elasticsearch/reference/current/search-aggregations-metrics-extendedstats-aggregation.html)
  - ```stdev``` - standard deviation
  - ```stdevmin``` - standard deviation lower boundary
  - ```stdevmax``` - standard deviation upper boundary
  - ```var``` - variance

## Example

```
nagios-elasticsearch \
  -t service:varnishlog \
  -t url:/ban \
  -k stopwatch.resp.duration_ms \
  -a max \
  -w 15.5 \
  -c 20 \
  --desc="Varnish ban duration" \
  --unit=ms
```

output:
```
OK: Varnishban duration OK | stopwatch.resp.duration_ms=20ms;30;60;0;
```

or (here with 15.5 as warning threshold):

```
WARNING: Varnish ban duration above warning threshold | stopwatch.resp.duration_ms=20.88190618681763ms;15.5;30;0;
```

or even (here with 20 as critical threshold):
```
CRITICAL: Varnish ban duration above critical threshold | stopwatch.resp.duration_ms=20.7244224537037ms;15.5;20;0;
```
