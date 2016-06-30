# ElasticSearch Nagios plugin
Generate alerts based on elasticsearch search query.

By providing and search query, a time range, a key (the key's value must be a number), this application will check the averaged result from the key's values over the time range specified (5min by default) and return a nagios-compatible alert.

## Example

```
nagios-elasticsearch \
  -q 'service:varnishlog AND url:"/ban"' \
  -k stopwatch.resp.duration_ms \
  -w 15.5 \
  -c 30 \
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
