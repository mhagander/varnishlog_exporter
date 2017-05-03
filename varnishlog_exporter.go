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
	"strings"
	"time"
)

/* Prometheus counters */
var promcounters = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "varnish_custom_counter",
		Help: "Varnish Custom counters",
	},
	[]string{"key"},
)
var promheaders = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "varnish_header_counter",
		Help: "Varnish Header counters",
	},
	[]string{"type", "header", "value"},
)

/* Accumulator goroutine to make sure we can have a small queue */
func log_accumulator(subchan chan string) {
	for {
		key := <-subchan
		promcounters.With(prometheus.Labels{"key": key}).Inc()
	}
}

type HeaderInfo struct {
	htype  string
	header string
	value  string
}

func header_accumulator(subchan chan HeaderInfo) {
	for {
		header := <-subchan
		promheaders.With(prometheus.Labels{"type": header.htype, "header": header.header, "value": header.value}).Inc()
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
	if has_headers {
		prometheus.MustRegister(promheaders)
	}

	// Separate accommulator routine
	log_subchan := make(chan string, 1000)
	go log_accumulator(log_subchan)

	header_subchan := make(chan HeaderInfo, 1000)
	if has_headers {
		go header_accumulator(header_subchan)
	}

	// Http listener
	go httpServer(*listenAddress, *metricsPath)

	for {
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
			vago.RAW,
			vago.COPT_TAIL|vago.COPT_BATCH,
			func(vxid uint32, tag, _type, data string) int {
				if tag == "VCL_Log" && strings.HasPrefix(data, "logkey:") {
					// the key is the rest of the string after :
					log_subchan <- data[7:]
				}
				if has_reqheaders && tag == "ReqHeader" {
					pieces := strings.SplitN(data, ": ", 2)
					if _, exists := reqheaders[pieces[0]]; exists {
						header_subchan <- HeaderInfo{htype: "req", header: pieces[0], value: pieces[1]}
					}
				}
				if has_respheaders && tag == "RespHeader" {
					pieces := strings.SplitN(data, ": ", 2)
					if _, exists := respheaders[pieces[0]]; exists {
						header_subchan <- HeaderInfo{htype: "resp", header: pieces[0], value: pieces[1]}
					}
				}

				// -1 : Stop after it finds the first record
				// >= 0 : Nothing to do but wait
				return 0
			})
		v.Close()
	}
}
