package main

import (
	"flag"
	"fmt"
	"github.com/phenomenes/vago"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

/* Prometheus counters */
var promcounters = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "varnish_custom_counter",
		Help: "Varnish Custom counters",
	},
	[]string{"key", "hitmiss"},
)
var promcountersizes = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "varnish_custom_size",
		Help: "Varnish Custom sizes",
	},
	[]string{"key", "hitmiss"},
)
var promheaders = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "varnish_header_counter",
		Help: "Varnish Header counters",
	},
	[]string{"type", "header", "value", "hitmiss"},
)
var promheadersizes = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "varnish_header_size",
		Help: "Varnish Header sizes",
	},
	[]string{"type", "header", "value", "hitmiss"},
)

/* Accumulator goroutine to make sure we can have a small queue */
func log_accumulator(subchan chan LogCollInfo) {
	for {
		lci := <-subchan
		promcounters.With(prometheus.Labels{"key": lci.logtag, "hitmiss": HITMISS_STRINGS[lci.hitmiss]}).Inc()
		promcountersizes.With(prometheus.Labels{"key": lci.logtag, "hitmiss": HITMISS_STRINGS[lci.hitmiss]}).Add(lci.size)
	}
}

type HeaderInfo struct {
	htype  string
	header string
	value  string
}

type LogCollInfo struct {
	logtag  string
	hitmiss int
	size    float64
}
type HeaderCollInfo struct {
	headerinfo HeaderInfo
	hitmiss    int
	size       float64
}

const (
	HITMISS_UNKNOWN    = 0
	HITMISS_HIT        = 1
	HITMISS_MISS       = 2
	HITMISS_HITFORPASS = 3
	HITMISS_PIPE       = 4
)

var HITMISS_STRINGS = []string{"UNKNOWN", "HIT", "MISS", "HITFORPASS", "PIPE"}

type SessionCollection struct {
	hitmiss int
	size    float64
	headers []HeaderInfo
	logtags []string
}

func header_accumulator(subchan chan HeaderCollInfo) {
	for {
		hci := <-subchan
		promheaders.With(prometheus.Labels{"type": hci.headerinfo.htype, "header": hci.headerinfo.header, "value": hci.headerinfo.value, "hitmiss": HITMISS_STRINGS[hci.hitmiss]}).Inc()
		promheadersizes.With(prometheus.Labels{"type": hci.headerinfo.htype, "header": hci.headerinfo.header, "value": hci.headerinfo.value, "hitmiss": HITMISS_STRINGS[hci.hitmiss]}).Add(hci.size)
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
type flagStringArray map[string]bool

func (i *flagStringArray) String() string {
	return "notused"
}

func (i flagStringArray) Set(value string) error {
	i[value] = true
	return nil
}

func main() {
	var reqheaders = make(flagStringArray)
	var respheaders = make(flagStringArray)
	flag.Var(&reqheaders, "reqheader", "Request header to include")
	flag.Var(&respheaders, "respheader", "Response header to include")

	var (
		listenAddress = flag.String("web.listen-address", ":9132", "Address to listen on for web interface and telemetry.")
		metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		varnishName   = flag.String("varnish.name", "", "Name of varnish instance to connect to.")
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

	prometheus.MustRegister(promcounters)
	prometheus.MustRegister(promcountersizes)
	if has_headers {
		prometheus.MustRegister(promheaders)
		prometheus.MustRegister(promheadersizes)
	}

	// Separate accommulator routine
	log_subchan := make(chan LogCollInfo, 1000)
	go log_accumulator(log_subchan)

	header_subchan := make(chan HeaderCollInfo, 1000)
	if has_headers {
		go header_accumulator(header_subchan)
	}

	// Http listener
	go httpServer(*listenAddress, *metricsPath)

	for {
		ctx := SessionCollection{}

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
				}
				if tag == "End" {
					for _, l := range ctx.logtags {
						log_subchan <- LogCollInfo{l, ctx.hitmiss, ctx.size}
					}
					for _, h := range ctx.headers {
						header_subchan <- HeaderCollInfo{h, ctx.hitmiss, ctx.size}
					}
				}
				if tag == "Hit" {
					ctx.hitmiss = HITMISS_HIT
				}
				if tag == "HitPass" {
					ctx.hitmiss = HITMISS_HITFORPASS
				}
				if tag == "VCL_call" {
					if data == "MISS" {
						ctx.hitmiss = HITMISS_MISS
					}
					if data == "PIPE" {
						ctx.hitmiss = HITMISS_PIPE
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
					pieces := strings.SplitN(data, ": ", 2)
					if _, exists := reqheaders[pieces[0]]; exists {
						ctx.headers = append(ctx.headers, HeaderInfo{htype: "req", header: pieces[0], value: pieces[1]})
					}
				}
				if has_respheaders && tag == "RespHeader" {
					pieces := strings.SplitN(data, ": ", 2)
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
