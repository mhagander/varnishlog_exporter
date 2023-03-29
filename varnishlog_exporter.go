package main

import (
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/varnishcache-friends/vago"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

/* Prometheus counters */
var promcounters, promcountersizes *prometheus.CounterVec
var promstatuses, promstatussizes *prometheus.CounterVec
var promversions, promversionsizes *prometheus.CounterVec
var promheaders, promheadersizes *prometheus.CounterVec
var promprobes *prometheus.SummaryVec

/* Accumulator goroutine to make sure we can have a small queue */
func log_accumulator(subchan chan LogCollInfo) {
	for {
		lci := <-subchan
		labels := prometheus.Labels{"key": lci.logtag, "hitmiss": HITMISS_STRINGS[lci.hitmiss]}
		if lci.statuscode != "" {
			labels["statuscode"] = lci.statuscode
		}
		promcounters.With(labels).Inc()
		promcountersizes.With(labels).Add(lci.size)
	}
}

func statuscode_accumulator(subchan chan CodeCollInfo) {
	for {
		cci := <-subchan
		labels := prometheus.Labels{"statuscode": cci.statuscode, "hitmiss": HITMISS_STRINGS[cci.hitmiss]}
		promstatuses.With(labels).Inc()
		promstatussizes.With(labels).Add(cci.size)
	}
}

func httpversion_accumulator(subchan chan VersionCollInfo) {
	for {
		vci := <-subchan
		labels := prometheus.Labels{"httpversion": vci.httpversion}
		promversions.With(labels).Inc()
		promversionsizes.With(labels).Add(vci.size)
	}
}

func probe_accumulator(subchan chan ProbeCollInfo) {
	for {
		pci := <-subchan
		promprobes.WithLabelValues(pci.probename).Observe(pci.responsetime)
	}
}

type HeaderInfo struct {
	htype  string
	header string
	value  string
}

type LogCollInfo struct {
	logtag     string
	hitmiss    int
	size       float64
	statuscode string
}
type HeaderCollInfo struct {
	headerinfo HeaderInfo
	hitmiss    int
	size       float64
	statuscode string
}

type CodeCollInfo struct {
	statuscode string
	hitmiss    int
	size       float64
}

type VersionCollInfo struct {
	httpversion string
	size        float64
}

type ProbeCollInfo struct {
	probename    string
	responsetime float64
}

const (
	HITMISS_UNKNOWN    = 0
	HITMISS_HIT        = 1
	HITMISS_MISS       = 2
	HITMISS_HITFORPASS = 3
	HITMISS_PASS       = 4
	HITMISS_PIPE       = 5
	HITMISS_SYNTH      = 6
)

var HITMISS_STRINGS = []string{"UNKNOWN", "HIT", "MISS", "HITFORPASS", "PASS", "PIPE", "SYNTH"}

type SessionCollection struct {
	hitmiss     int
	size        float64
	statuscode  string
	httpversion string
	headers     []HeaderInfo
	logtags     []string
}

func header_accumulator(subchan chan HeaderCollInfo) {
	for {
		hci := <-subchan
		labels := prometheus.Labels{"type": hci.headerinfo.htype, "header": hci.headerinfo.header, "value": hci.headerinfo.value, "hitmiss": HITMISS_STRINGS[hci.hitmiss]}
		if hci.statuscode != "" {
			labels["statuscode"] = hci.statuscode
		}
		promheaders.With(labels).Inc()
		promheadersizes.With(labels).Add(hci.size)
	}
}

/* Webserver goroutine that servers up the current metrics */
func httpServer(listenAddress string, metricsPath string) {
	http.Handle(metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Varnishlog Exporter</title></head>
             <body>
             <h1>Varnishlog Exporter</h1>
             <p><a href='` + metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})
	http.ListenAndServe(listenAddress, nil)
}

/* Map options for headers, for quick lookup */
type flagLowerStringArray map[string]bool

func (i *flagLowerStringArray) String() string {
	return "notused"
}

func (i flagLowerStringArray) Set(value string) error {
	i[strings.ToLower(value)] = true
	return nil
}

func main() {
	var reqheaders = make(flagLowerStringArray)
	var respheaders = make(flagLowerStringArray)
	flag.Var(&reqheaders, "reqheader", "Request header to include")
	flag.Var(&respheaders, "respheader", "Response header to include")

	var (
		listenAddress = flag.String("web.listen-address", ":9132", "Address to listen on for web interface and telemetry.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		varnishName   = flag.String("varnish.name", "", "Name of varnish instance to connect to.")
		statusCodes   = flag.Bool("statuscodes", false, "Include statistics per statuscode")
		httpVersions  = flag.Bool("httpversions", false, "Include statistics per http version")
		probes        = flag.Bool("probes", false, "Include statitics for probes")
		showVersion   = flag.Bool("version", false, "Print version information.")
		debug         = flag.Bool("debug", false, "Print debugging information (lots!).")
	)
	flag.Parse()

	has_reqheaders := len(reqheaders) > 0
	has_respheaders := len(respheaders) > 0
	has_headers := has_reqheaders || has_respheaders

	if *showVersion {
		fmt.Println(version.Print("varnishlog_exporter"))
		os.Exit(0)
	}

	counterlabels := []string{"key", "hitmiss"}
	headerlabels := []string{"type", "header", "value", "hitmiss"}
	if *statusCodes {
		counterlabels = append(counterlabels, "statuscode")
		headerlabels = append(headerlabels, "statuscode")
	}
	promcounters = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "varnish_custom_counter",
			Help: "Varnish Custom counters",
		},
		counterlabels,
	)
	promcountersizes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "varnish_custom_size",
			Help: "Varnish Custom sizes",
		},
		counterlabels,
	)
	if *statusCodes {
		promstatuses = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "varnish_statuscode_counter",
				Help: "Varnish Statuscode counters",
			},
			[]string{"statuscode", "hitmiss"},
		)
		promstatussizes = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "varnish_statuscode_size",
				Help: "Varnish Statuscode sizes",
			},
			[]string{"statuscode", "hitmiss"},
		)
	}
	if *httpVersions {
		promversions = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "varnish_httpversion_counter",
				Help: "Varnish httpversion counters",
			},
			[]string{"httpversion"},
		)
		promversionsizes = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "varnish_httpversion_size",
				Help: "Varnish httpversion sizes",
			},
			[]string{"httpversion"},
		)
	}
	if *probes {
		promprobes = prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Name: "varnish_probe_responsetime",
				Help: "Response time for varnish probes",
			},
			[]string{"name"},
		)
	}
	promheaders = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "varnish_header_counter",
			Help: "Varnish Header counters",
		},
		headerlabels,
	)
	promheadersizes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "varnish_header_size",
			Help: "Varnish Header sizes",
		},
		headerlabels,
	)

	prometheus.MustRegister(promcounters)
	prometheus.MustRegister(promcountersizes)
	if *statusCodes {
		prometheus.MustRegister(promstatuses)
		prometheus.MustRegister(promstatussizes)
	}
	if *httpVersions {
		prometheus.MustRegister(promversions)
		prometheus.MustRegister(promversionsizes)
	}
	if *probes {
		prometheus.MustRegister(promprobes)
	}
	if has_headers {
		prometheus.MustRegister(promheaders)
		prometheus.MustRegister(promheadersizes)
	}

	// Separate accommulator routines
	log_subchan := make(chan LogCollInfo, 1000)
	go log_accumulator(log_subchan)

	var statuscode_subchan chan CodeCollInfo
	if *statusCodes {
		statuscode_subchan = make(chan CodeCollInfo, 1000)
		go statuscode_accumulator(statuscode_subchan)
	}

	var httpversion_subchan chan VersionCollInfo
	if *httpVersions {
		httpversion_subchan = make(chan VersionCollInfo, 1000)
		go httpversion_accumulator(httpversion_subchan)
	}

	header_subchan := make(chan HeaderCollInfo, 1000)
	if has_headers {
		go header_accumulator(header_subchan)
	}

	// Http listener
	go httpServer(*listenAddress, *metricsPath)

	if *probes {
		// When collecting probes we run a separate goroutine that parses the VSL
		// independently, because we need to parse it in RAW mode, not grouped by
		// request.
		go func() {
			var probe_subchan chan ProbeCollInfo
			if *probes {
				probe_subchan = make(chan ProbeCollInfo, 1000)
				go probe_accumulator(probe_subchan)
			}

			for {
				c := vago.Config{}
				c.Path = *varnishName
				v, err := vago.Open(&c)
				if err != nil {
					fmt.Println(err)
					time.Sleep(5 * time.Second)
					continue
				}

				v.Log("",
					vago.RAW,
					vago.COPT_TAIL|vago.COPT_BATCH,
					func(vxid uint32, tag, _type, data string) int {
						if tag == "Backend_health" {
							fmt.Printf("vxid %d, tag %s, type %s, data %s\n", vxid, tag, _type, data)
							pieces := strings.Split(data, " ")
							timing, err := strconv.ParseFloat(pieces[7], 64)
							if err == nil {
								probe_subchan <- ProbeCollInfo{pieces[0][strings.LastIndex(pieces[0], ".")+1:], timing}
							}
						}

						return 0
					},
				)
			}
		}()
	}

	for {
		ctx := SessionCollection{}
		collecting := false

		// Open the default Varnish Shared Memory file
		c := vago.Config{}
		c.Path = *varnishName

		v, err := vago.Open(&c)
		if err != nil {
			fmt.Println(err)
			time.Sleep(5 * time.Second)
			continue
		}
		v.Log("",
			vago.REQ,
			vago.COPT_TAIL|vago.COPT_BATCH,
			func(vxid uint32, tag, _type, data string) int {
				if *debug {
					fmt.Printf("vxid %d, tag %s, type %s, data %s\n", vxid, tag, _type, data)
				}

				if tag == "Begin" && strings.HasPrefix(data, "req") {
					ctx = SessionCollection{}
					collecting = true
				}
				if tag == "End" {
					if collecting {
						for _, l := range ctx.logtags {
							log_subchan <- LogCollInfo{l, ctx.hitmiss, ctx.size, ctx.statuscode}
						}
						for _, h := range ctx.headers {
							header_subchan <- HeaderCollInfo{h, ctx.hitmiss, ctx.size, ctx.statuscode}
						}
						if *statusCodes {
							statuscode_subchan <- CodeCollInfo{ctx.statuscode, ctx.hitmiss, ctx.size}
						}
						if *httpVersions {
							httpversion_subchan <- VersionCollInfo{ctx.httpversion, ctx.size}
						}
					}
					collecting = false
				}
				if *statusCodes && tag == "RespStatus" {
					ctx.statuscode = data
				}
				if *httpVersions && tag == "ReqProtocol" {
					ctx.httpversion = data
				}
				if tag == "Hit" {
					ctx.hitmiss = HITMISS_HIT
				}
				if tag == "HitPass" {
					ctx.hitmiss = HITMISS_HITFORPASS
				}
				if tag == "VCL_call" {
					if data == "PASS" && ctx.hitmiss != HITMISS_HITFORPASS {
						ctx.hitmiss = HITMISS_PASS
					}
					if data == "MISS" {
						ctx.hitmiss = HITMISS_MISS
					}
					if data == "PIPE" {
						ctx.hitmiss = HITMISS_PIPE
					}
					if data == "SYNTH" {
						ctx.hitmiss = HITMISS_SYNTH
					}
				}
				if tag == "ReqAcct" {
					pieces := strings.Split(data, " ")
					ctx.size, _ = strconv.ParseFloat(pieces[5], 64)
				}

				if tag == "VCL_Log" && strings.HasPrefix(data, "logkey:") {
					// the key is the rest of the string after :
					ctx.logtags = append(ctx.logtags, data[7:])
				}
				if has_reqheaders && tag == "ReqHeader" {
					pieces := strings.SplitN(strings.ToLower(data), ": ", 2)
					if _, exists := reqheaders[pieces[0]]; exists {
						ctx.headers = append(ctx.headers, HeaderInfo{htype: "req", header: pieces[0], value: pieces[1]})
					}
				}
				if has_respheaders && tag == "RespHeader" {
					pieces := strings.SplitN(strings.ToLower(data), ": ", 2)
					if _, exists := respheaders[pieces[0]]; exists {
						ctx.headers = append(ctx.headers, HeaderInfo{htype: "resp", header: pieces[0], value: pieces[1]})
					}
				}

				// -1 : Stop after it finds the first record
				// >= 0 : Nothing to do but wait
				return 0
			})
		v.Close()
	}
}
