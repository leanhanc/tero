// Command fakeprobe stands in for echoip's port check on the test server:
// GET /port/<n> connects to 127.0.0.1:<n> and answers as echoip does, with
// the made-up public IP the suite gives init. Ports in -closed are always
// reported closed, as a provider firewall would make them.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9901", "address to listen on")
	publicIP := flag.String("ip", "", "public IP to answer with")
	closedList := flag.String("closed", "", "comma-separated ports to report closed")
	flag.Parse()

	closed := strings.Split(*closedList, ",")

	http.HandleFunc("GET /port/{port}", func(w http.ResponseWriter, r *http.Request) {
		port, err := strconv.Atoi(r.PathValue("port"))
		if err != nil || port < 1 || port > 65535 {
			http.Error(w, "invalid port", http.StatusBadRequest)
			return
		}

		isReachable := !slices.Contains(closed, r.PathValue("port")) && canConnect(port)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ip": *publicIP, "port": port, "reachable": isReachable})
	})

	log.Fatal(http.ListenAndServe(*listen, nil))
}

func canConnect(port int) bool {
	connection, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	connection.Close()
	return true
}
