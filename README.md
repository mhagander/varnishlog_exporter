# varnishlog_exporter

## Summary

`varnishlog_exporter` is an exporter for `prometheus` that counts
occurrences in the varnish shared memory log of specific tags and
headers and reports the accumulated counts.

## Exported metrics

### Cstom log metrics

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

(with each having the count of the number of requests matching it).

### Tracking status codes

If `-statuscodes` is included on the commandline, per-statuscode
statistics are collected. These are both collected as a label on
`varnish_header_counter`, and as a global metrics with the total
number of requests and size.

### Tracking http versions

If `-httpversions` is included on the commandline, metrics are
collected for http versions. This is done by looking at the
`ReqProtocol` value in Varnish, and will collect on number of requests
and total size of those requests.

### Tracking probe response times

If `-probes` is included on the commandline, metrics are collected for
probe response times. Each probe will be recorded as a Summary,
meaning it will get a count and a sum.

## Usage

	-httpversions
		Include statistics per http version
	-probes
		Inlcude probe statistics
    -reqheader value
      	Request header to include
    -respheader value
      	Response header to include
	-statuscodes
		Include statistics per statuscode
    -varnish.name string
      	Name of varnish instance to connect to.
    -version
      	Print version information.
    -web.listen-address string
      	Address to listen on for web interface and telemetry. (default ":9132")
    -web.telemetry-path string
      	Path under which to expose metrics. (default "/metrics")
