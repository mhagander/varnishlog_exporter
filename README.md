# varnishlog_exporter

## Summary

`varnishlog_exporter` is an exporter for `prometheus` that counts
occurrences in the varnish shared memory log of specific tags and
headers and reports the accumulated counts.

## Exported metrics

`varnishlog_exporter` can export 3 types of metrics. For each type,
multiple counters can be exported, and will be created dynamically
based on the data that shows up in the log.

### custom log metrics

At any point in the VCL code, it can be decorated with a log message
of the format `logkey:something`. This will generate a metric of type
`varnish_custom_counter` with the label `key` set to `something`. This
gives maximum flexibility, and in VCL it's possible to write something
like:

```
import std;

sub vcl_recv {
   ...
   if (...) {
      std.log('logkey:awesome');
   }
}
```

to track specific occurrences.

### Request and response header metrics

If specific headers are added to the commandline using `-reqheader` or
`-respheader` (for request headers and response headers respectively),
these headers will be automatically tracked. If no header is specified
on the commandline, none will be tracked (as tracking all headers
would likely lead to extreme metrics bloat). These headers will be
exported as metrics of type `varnish_header_counter`. Each metric will
get a label `type` set to either `req` or `resp`, a label `header`
with the name of the header, and a label `value` with the unique
value.

For example, something like:

```
$ varnishlog_exporter -respheader Server -respheader Content-Type
```

will export for example:
```
varnish_header_counter{header="Server",type="resp",value="nginx/1.4.6 (Ubuntu)"}
varnish_header_counter{header="Content-Type",type="resp",value="text/html"}
varnish_header_counter{header="Content-Type",type="resp",value="text/plain"}
```

(with each having the count of of the number of requests matching it).

## Usage

    -reqheader value
      	Request header to include
    -respheader value
      	Response header to include
    -varnish.name string
      	Name of varnish instance to connect to.
    -version
      	Print version information.
    -web.listen-address string
      	Address to listen on for web interface and telemetry. (default ":9132")
    -web.telemetry-path string
      	Path under which to expose metrics. (default "/metrics")
